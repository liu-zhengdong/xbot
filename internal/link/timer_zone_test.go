package link

import (
	"path/filepath"
	"testing"
	"time"
)

func writeRoutine(t *testing.T, dir string, r Routine) {
	t.Helper()
	if err := (&Timer{Dir: dir}).Save(r); err != nil {
		t.Fatal(err)
	}
}

func TestTimerZones(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	dir := filepath.Join(t.TempDir(), "routines")
	writeRoutine(t, dir, Routine{ID: "legacy", Name: "legacy", Prompt: "p", Cron: "0 1 * * *", Enabled: true})
	writeRoutine(t, dir, Routine{ID: "local", Name: "local", Prompt: "p", Cron: "0 1 * * *", Zone: "local", Enabled: true})
	tm := &Timer{Dir: dir, Loc: loc}
	fire := func(when time.Time) []Event {
		t.Helper()
		var posted []Event
		if err := tm.Fire(when, func(ev Event) error {
			posted = append(posted, ev)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return posted
	}
	if got := fire(time.Date(2026, 10, 17, 17, 0, 0, 0, time.UTC)); len(got) != 1 || got[0].ID != "rt_local_20261017T1700" {
		t.Fatalf("local should fire at 17:00Z: %+v", got)
	}
	if got := fire(time.Date(2026, 10, 18, 1, 0, 0, 0, time.UTC)); len(got) != 1 || got[0].ID != "rt_legacy_20261018T0100" {
		t.Fatalf("legacy should fire at 01:00Z: %+v", got)
	}
}
