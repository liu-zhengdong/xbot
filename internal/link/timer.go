package link

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"xbot/internal/cron"
	"xbot/internal/fsutil"
	"xbot/internal/tools"
)

// Routine is a saved prompt plus either a 5-field cron expression or a one-shot At time.
// At is RFC3339. A one-shot is exempt from the minimum cron gap and disables itself after it fires.
type Routine struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Prompt   string `json:"prompt"`
	Cron     string `json:"cron,omitempty"`
	At       string `json:"at,omitempty"`
	Enabled  bool   `json:"enabled"`
	LastSlot string `json:"last_slot,omitempty"`
	// Zone is "local" for the computer's local zone, "UTC" for a pinned UTC
	// schedule, or "" for a legacy file written before C1 (also UTC).
	Zone string `json:"zone,omitempty"`
}

// OneShotWindow is how late a poll may be and still fire a one-shot.
const OneShotWindow = 15 * time.Second

var routineID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// Timer emits routine events and stores them under a bot directory.
type Timer struct {
	Dir      string
	Sender   string
	Interval time.Duration
	// Loc is the computer's local zone for routines with Zone "local".
	// Nil means time.Local.
	Loc *time.Location
}

func (t *Timer) Name() string        { return NameTimer }
func (t *Timer) Tools() []tools.Tool { return nil }

func (t *Timer) every() time.Duration {
	if t.Interval > 0 {
		return t.Interval
	}
	return 15 * time.Second
}

// Location is the zone used for routines with Zone "local".
func (t *Timer) Location() *time.Location { return t.loc() }

func (t *Timer) loc() *time.Location {
	if t.Loc != nil {
		return t.Loc
	}
	return time.Local
}

func (t *Timer) sender() string {
	if t.Sender != "" {
		return t.Sender
	}
	return "timer"
}

// Run polls until ctx is cancelled. A repeated slot is one event id, so a second poster does not double-wake.
func (t *Timer) Run(ctx context.Context, emit func(Event) error) error {
	tick := time.NewTicker(t.every())
	defer tick.Stop()
	for {
		if err := t.Fire(time.Now(), emit); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

// Due lists routines whose cron matches now and whose slot has not been committed.
func (t *Timer) Due(now time.Time) ([]Event, error) {
	list, err := t.List()
	if err != nil {
		return nil, err
	}
	now = now.UTC()
	slot := now.Format("20060102T1504")
	var out []Event
	for _, r := range list {
		if !r.Enabled {
			continue
		}
		if r.At != "" {
			ev, ok := dueOnce(r, now, t.sender())
			if ok {
				out = append(out, ev)
			}
			continue
		}
		sched, err := cron.Parse(r.Cron)
		if err != nil {
			continue
		}
		var zoneLoc *time.Location
		switch r.Zone {
		case "local":
			zoneLoc = t.loc()
		case "", "UTC":
			zoneLoc = time.UTC
		default:
			continue
		}
		if !sched.MatchIn(now, zoneLoc) || r.LastSlot == slot {
			continue
		}
		ev := NewEvent(NameTimer, TypeTimer, routineBody(r), t.sender(), now)
		ev.ID = "rt_" + r.ID + "_" + slot
		out = append(out, ev)
	}
	return out, nil
}

func dueOnce(r Routine, now time.Time, sender string) (Event, bool) {
	at, err := time.Parse(time.RFC3339, r.At)
	if err != nil {
		return Event{}, false
	}
	at = at.UTC()
	if now.Before(at) || now.Sub(at) > OneShotWindow {
		return Event{}, false
	}
	slot := at.Format("20060102T1504")
	if r.LastSlot == slot {
		return Event{}, false
	}
	ev := NewEvent(NameTimer, TypeTimer, routineBody(r), sender, now)
	ev.ID = "rt_" + r.ID + "_" + slot
	return ev, true
}

// Fire posts due events and commits their slots.
func (t *Timer) Fire(now time.Time, emit func(Event) error) error {
	evs, err := t.Due(now)
	if err != nil {
		return err
	}
	for _, ev := range evs {
		if err := emit(ev); err != nil {
			return err
		}
		if err := t.Commit(ev); err != nil {
			return err
		}
	}
	return nil
}

// Commit records the slot so the same minute does not fire again after a restart.
func (t *Timer) Commit(ev Event) error {
	id, slot, err := splitRoutineID(ev.ID)
	if err != nil {
		return err
	}
	return t.Update(id, func(r *Routine) error {
		r.LastSlot = slot
		if r.At != "" {
			r.Enabled = false
		}
		return nil
	})
}

func splitRoutineID(eventID string) (string, string, error) {
	rest := strings.TrimPrefix(eventID, "rt_")
	i := strings.LastIndex(rest, "_")
	if !strings.HasPrefix(eventID, "rt_") || i <= 0 || i == len(rest)-1 {
		return "", "", fmt.Errorf("timer: bad routine event %q", eventID)
	}
	return rest[:i], rest[i+1:], nil
}

func routineBody(r Routine) string {
	return RoutineBody(r.ID, r.Name, r.Prompt)
}

// RoutineBody wraps a routine prompt with the scheduled-wake tail. A routine's
// own instruction for the empty case wins over the default SILENT reply.
func RoutineBody(id, name, prompt string) string {
	return fmt.Sprintf("<routine id=%q name=%q>\n%s\n</routine>\nThis is a scheduled wake. Use memory. If the owner should hear about it, call notify_owner. If the routine says what to send when there is nothing (for example \"if nothing, say X\") and you checked and found nothing, send X with notify_owner; do not reply SILENT. Only when the routine gives no such instruction and nothing is worth saying, reply with exactly SILENT and do not call notify_owner.", id, name, prompt)
}

// List reads every routine file, sorted by id. A zone that a pre-C1 binary
// dropped is restored from the zone index.
func (t *Timer) List() ([]Routine, error) {
	list, err := t.listRaw()
	if err != nil {
		return nil, err
	}
	zones := t.knownZones()
	for i := range list {
		list[i] = restoreZone(list[i], zones)
	}
	return list, nil
}

// listRaw reads every routine file as stored, sorted by id.
func (t *Timer) listRaw() ([]Routine, error) {
	entries, err := os.ReadDir(t.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Routine
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		r, err := t.read(filepath.Join(t.Dir, ent.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Load reads one routine.
func (t *Timer) Load(id string) (Routine, error) {
	if !routineID.MatchString(id) {
		return Routine{}, fmt.Errorf("timer: bad id %q", id)
	}
	r, err := t.read(filepath.Join(t.Dir, id+".json"))
	if err != nil {
		return Routine{}, err
	}
	return restoreZone(r, t.knownZones()), nil
}

// knownZones reads the zone index for restoreZone. An unreadable index only
// means no restore; the routines themselves still load.
func (t *Timer) knownZones() map[string]zoneEntry {
	zones, err := t.loadZones()
	if err != nil {
		return nil
	}
	return zones
}

// restoreZone gives back the zone a pre-C1 binary dropped when it rewrote the
// file: same id and exactly the cron this package last saved. Otherwise a
// zone-less file stays legacy UTC.
func restoreZone(r Routine, zones map[string]zoneEntry) Routine {
	if r.Zone != "" {
		return r
	}
	if ent, ok := zones[r.ID]; ok && ent.Cron == r.Cron && ent.Zone != "" {
		r.Zone = ent.Zone
	}
	return r
}

func (t *Timer) read(path string) (Routine, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Routine{}, err
	}
	var r Routine
	if err := json.Unmarshal(raw, &r); err != nil {
		return Routine{}, err
	}
	return r, nil
}

// Save validates the routine and writes it under the directory lock.
func (t *Timer) Save(r Routine) error {
	return t.withLock(func() error { return t.saveLocked(r) })
}

// Update re-reads a routine under the directory lock, applies fn, and saves it.
func (t *Timer) Update(id string, fn func(*Routine) error) error {
	return t.withLock(func() error {
		r, err := t.Load(id)
		if err != nil {
			return err
		}
		if err := fn(&r); err != nil {
			return err
		}
		return t.saveLocked(r)
	})
}

func (t *Timer) withLock(fn func() error) error {
	lock, err := fsutil.Lock(filepath.Join(t.Dir, ".lock"))
	if err != nil {
		return err
	}
	defer lock.Release()
	return fn()
}

// saveLocked validates and writes a routine. The caller holds the directory lock.
func (t *Timer) saveLocked(r Routine) error {
	if !routineID.MatchString(r.ID) {
		return fmt.Errorf("timer: bad id %q", r.ID)
	}
	r.Prompt = strings.TrimSpace(r.Prompt)
	if r.Prompt == "" {
		return errors.New("timer: routine prompt is empty")
	}
	r.At = strings.TrimSpace(r.At)
	r.Cron = strings.TrimSpace(r.Cron)
	switch r.Zone {
	case "", "local", "UTC":
	default:
		return fmt.Errorf("timer: bad zone %q", r.Zone)
	}
	switch {
	case r.At != "" && r.Cron != "":
		return errors.New("timer: set at or cron, not both")
	case r.At != "":
		at, err := time.Parse(time.RFC3339, r.At)
		if err != nil {
			return fmt.Errorf("timer: at: %w", err)
		}
		r.At = at.UTC().Format(time.RFC3339)
	default:
		if err := cron.Allow(r.Cron); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(t.Dir, 0700); err != nil {
		return err
	}
	raw, err := fsutil.Marshal(r)
	if err != nil {
		return err
	}
	if err := fsutil.WriteAtomic(filepath.Join(t.Dir, r.ID+".json"), raw, 0600); err != nil {
		return err
	}
	if r.Zone == "" {
		return nil
	}
	return t.saveZone(r.ID, zoneEntry{Cron: r.Cron, Zone: r.Zone})
}

// Delete removes a routine file and its zone index entry. A missing file is not an error.
func (t *Timer) Delete(id string) error {
	if !routineID.MatchString(id) {
		return fmt.Errorf("timer: bad id %q", id)
	}
	return t.withLock(func() error {
		err := os.Remove(filepath.Join(t.Dir, id+".json"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		zones, err := t.loadZones()
		if err != nil {
			return err
		}
		if _, ok := zones[id]; !ok {
			return nil
		}
		delete(zones, id)
		return t.writeZones(zones)
	})
}

// zoneEntry remembers the zone and cron this package wrote, so a routine file
// rewritten by a pre-C1 binary is not converted a second time.
type zoneEntry struct {
	Cron string `json:"cron"`
	Zone string `json:"zone"`
}

func (t *Timer) zonePath() string { return filepath.Join(t.Dir, ".zones") }

func (t *Timer) loadZones() (map[string]zoneEntry, error) {
	raw, err := os.ReadFile(t.zonePath())
	if errors.Is(err, os.ErrNotExist) {
		return map[string]zoneEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	zones := map[string]zoneEntry{}
	if err := json.Unmarshal(raw, &zones); err != nil {
		return nil, fmt.Errorf("timer: %s: %w", t.zonePath(), err)
	}
	return zones, nil
}

func (t *Timer) saveZone(id string, ent zoneEntry) error {
	zones, err := t.loadZones()
	if err != nil {
		return err
	}
	if old, ok := zones[id]; ok && old == ent {
		return nil
	}
	zones[id] = ent
	return t.writeZones(zones)
}

func (t *Timer) writeZones(zones map[string]zoneEntry) error {
	raw, err := fsutil.Marshal(zones)
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(t.zonePath(), raw, 0600)
}
