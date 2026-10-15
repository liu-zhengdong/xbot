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

// Routine is a saved prompt plus a 5-field cron expression.
type Routine struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Prompt   string `json:"prompt"`
	Cron     string `json:"cron"`
	Enabled  bool   `json:"enabled"`
	LastSlot string `json:"last_slot,omitempty"`
}

var routineID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// Timer emits routine events and stores them under a bot directory.
type Timer struct {
	Dir      string
	Sender   string
	Interval time.Duration
}

func (t *Timer) Name() string        { return NameTimer }
func (t *Timer) Tools() []tools.Tool { return nil }

func (t *Timer) every() time.Duration {
	if t.Interval > 0 {
		return t.Interval
	}
	return 15 * time.Second
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
		sched, err := cron.Parse(r.Cron)
		if err != nil || !sched.Match(now) || r.LastSlot == slot {
			continue
		}
		ev := NewEvent(NameTimer, TypeTimer, routineBody(r), t.sender(), now)
		ev.ID = "rt_" + r.ID + "_" + slot
		out = append(out, ev)
	}
	return out, nil
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
	r, err := t.Load(id)
	if err != nil {
		return err
	}
	r.LastSlot = slot
	return t.Save(r)
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
	return fmt.Sprintf("<routine id=%q name=%q>\n%s\n</routine>\nThis is a scheduled wake. Use memory. If the owner should hear about it, call notify_owner. If nothing is worth saying, reply with exactly SILENT and do not call notify_owner.", r.ID, r.Name, r.Prompt)
}

// List reads every routine file, sorted by id.
func (t *Timer) List() ([]Routine, error) {
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
	return t.read(filepath.Join(t.Dir, id+".json"))
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

// Save validates the cron gap and writes the routine.
func (t *Timer) Save(r Routine) error {
	if !routineID.MatchString(r.ID) {
		return fmt.Errorf("timer: bad id %q", r.ID)
	}
	r.Prompt = strings.TrimSpace(r.Prompt)
	if r.Prompt == "" {
		return errors.New("timer: routine prompt is empty")
	}
	if err := cron.Allow(r.Cron); err != nil {
		return err
	}
	if err := os.MkdirAll(t.Dir, 0700); err != nil {
		return err
	}
	raw, err := fsutil.Marshal(r)
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(filepath.Join(t.Dir, r.ID+".json"), raw, 0600)
}

// Delete removes a routine file. A missing file is not an error.
func (t *Timer) Delete(id string) error {
	if !routineID.MatchString(id) {
		return fmt.Errorf("timer: bad id %q", id)
	}
	err := os.Remove(filepath.Join(t.Dir, id+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
