package proc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"xbot/internal/cron"
	"xbot/internal/fsutil"
	"xbot/internal/link"
)

const missedWindow = 7 * 24 * time.Hour

// watermarkEvery limits watermark writes; the chat loop polls every 200ms.
const watermarkEvery = 15 * time.Second

// NoticeMissed posts one summary of skipped slots and advances the shared watermark.
func NoticeMissed(tm *link.Timer, post func(link.Event) error, now time.Time) error {
	dir := filepath.Dir(tm.Dir)
	path := filepath.Join(dir, "timer-watermark")
	now = now.UTC()
	if wm, ok, err := readWatermark(path); err == nil && ok && !now.Before(wm) && now.Sub(wm) < watermarkEvery {
		return nil
	}
	lock, err := fsutil.Lock(filepath.Join(dir, "timer-watermark.lock"))
	if err != nil {
		return err
	}
	defer lock.Release()
	wm, ok, err := readWatermark(path)
	if err != nil {
		return err
	}
	if ok && now.Before(wm) {
		return nil
	}
	if ok && now.Sub(wm) > link.OneShotWindow {
		sender := tm.Sender
		if sender == "" {
			sender = "timer"
		}
		ev, err := missedNotice(tm, wm, now, sender)
		if err != nil {
			return err
		}
		if ev != nil {
			if err := post(*ev); err != nil {
				return err
			}
		}
	}
	return writeWatermark(path, now)
}

func routineName(name string) string {
	runes := []rune(name)
	for i, r := range runes {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			runes[i] = ' '
		}
	}
	if len(runes) > 80 {
		return string(runes[:80]) + "…"
	}
	return string(runes)
}

func missedNotice(tm *link.Timer, wm, now time.Time, sender string) (*link.Event, error) {
	since := wm
	truncated := now.Sub(wm) > missedWindow
	if truncated {
		since = now.Add(-missedWindow)
	}
	slots, err := missedSlots(tm, since, now.Truncate(time.Minute), now)
	if err != nil {
		return nil, err
	}
	if len(slots) == 0 {
		return nil, nil
	}
	ev := link.NewEvent(link.NameTimer, link.TypeTimer, missedBody(slots, since, now, truncated, tm.Location()), sender, now)
	ev.ID = "missed_" + wm.UTC().Format("20060102T150405")
	return &ev, nil
}

func missedBody(slots []missedSlot, since, now time.Time, truncated bool, loc *time.Location) string {
	type group struct {
		name  string
		times []time.Time
	}
	groups := map[string]*group{}
	var ids []string
	for _, s := range slots {
		g, ok := groups[s.ID]
		if !ok {
			g = &group{name: s.Name}
			groups[s.ID] = g
			ids = append(ids, s.ID)
		}
		g.times = append(g.times, s.At)
	}
	sort.Strings(ids)

	const maxRoutines, maxTimes = 20, 5
	var b strings.Builder
	fmt.Fprintf(&b, "Missed routine times from %s to %s:\n", since.In(loc).Format(time.RFC3339), now.In(loc).Format(time.RFC3339))
	for i, id := range ids {
		if i >= maxRoutines {
			fmt.Fprintf(&b, "- … and %d more routines\n", len(ids)-maxRoutines)
			break
		}
		g := groups[id]
		fmt.Fprintf(&b, "- %q %q: %d missed (", routineName(id), routineName(g.name), len(g.times))
		shown := len(g.times)
		if shown > maxTimes {
			shown = maxTimes
		}
		for j := 0; j < shown; j++ {
			if j > 0 {
				b.WriteString(", ")
			}
			b.WriteString(g.times[j].In(loc).Format("2006-01-02 15:04"))
		}
		if len(g.times) > maxTimes {
			b.WriteString(", …")
		}
		b.WriteString(")\n")
	}
	if truncated {
		b.WriteString("Only the last 7 days were checked.\n")
	}
	b.WriteString("The computer was off or asleep. These routine times were missed and were not run. Decide whether anything is still worth doing now; if nothing is, reply with exactly SILENT and do not call notify_owner.")
	return b.String()
}

func readWatermark(path string) (time.Time, bool, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(raw)))
	if err != nil {
		return time.Time{}, false, nil
	}
	return t.UTC(), true, nil
}

func writeWatermark(path string, now time.Time) error {
	return fsutil.WriteAtomic(path, []byte(now.UTC().Format(time.RFC3339)+"\n"), 0600)
}

// missedSlot is one scheduled time a routine skipped while the computer was off
// or asleep. At is the slot instant, in UTC.
type missedSlot struct {
	ID   string
	Name string
	At   time.Time
}

// missedSlots lists enabled routines' slots strictly after since and strictly before
// until, using the same minute and zone rules as Due. Callers guarantee the
// window is at most 7 days. The result is sorted by At then ID.
func missedSlots(t *link.Timer, since, until, now time.Time) ([]missedSlot, error) {
	list, err := t.List()
	if err != nil {
		return nil, err
	}
	since = since.UTC()
	until = until.UTC()
	if !until.After(since) {
		return nil, nil
	}
	var out []missedSlot
	for _, r := range list {
		if !r.Enabled {
			continue
		}
		if r.At != "" {
			at, err := time.Parse(time.RFC3339, r.At)
			if err != nil {
				continue
			}
			at = at.UTC()
			if !at.After(since) || !at.Before(until) || now.Sub(at) <= link.OneShotWindow {
				continue
			}
			if r.LastSlot == at.Format("20060102T1504") {
				continue
			}
			out = append(out, missedSlot{ID: r.ID, Name: r.Name, At: at})
			continue
		}
		sched, err := cron.Parse(r.Cron)
		if err != nil {
			continue
		}
		var zoneLoc *time.Location
		switch r.Zone {
		case "local":
			zoneLoc = t.Location()
		case "", "UTC":
			zoneLoc = time.UTC
		default:
			continue
		}
		// The first whole minute strictly after since; a slot exactly at since
		// is not missed.
		for m := since.Truncate(time.Minute).Add(time.Minute); m.Before(until); m = m.Add(time.Minute) {
			if !sched.MatchIn(m, zoneLoc) || r.LastSlot == m.Format("20060102T1504") {
				continue
			}
			if r.Zone == "local" && !sched.EveryHour() && repeatedWall(m, zoneLoc) {
				continue
			}
			out = append(out, missedSlot{ID: r.ID, Name: r.Name, At: m})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.Before(out[j].At)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// repeatedWall mirrors the timer's DST rule: inside the repeated hour after
// daylight saving ends, a fixed local wall time counts once.
func repeatedWall(now time.Time, loc *time.Location) bool {
	_, off := now.In(loc).Zone()
	for _, d := range []time.Duration{30 * time.Minute, time.Hour, 2 * time.Hour} {
		if _, prev := now.Add(-d).In(loc).Zone(); prev-off == int(d/time.Second) {
			return true
		}
	}
	return false
}
