package link

import (
	"path/filepath"
	"testing"
	"time"
	_ "time/tzdata"
)

func TestMissedOneShotIsDisabled(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "routines")
	tm := &Timer{Dir: dir}
	at := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	missedAt := at.Format(time.RFC3339)
	if err := tm.Save(Routine{ID: "missed", Name: "m", Prompt: "p", At: missedAt, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	var posted []Event
	if err := tm.Fire(at.Add(time.Hour), func(ev Event) error {
		posted = append(posted, ev)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(posted) != 0 {
		t.Fatalf("late one-shot fired: %+v", posted)
	}
	got, err := tm.Load("missed")
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.LastSlot != "" {
		t.Fatalf("missed one-shot %+v", got)
	}
	future := at.Add(24 * time.Hour)
	if err := tm.Save(Routine{ID: "future", Name: "f", Prompt: "p", At: future.Format(time.RFC3339), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := tm.Fire(at.Add(time.Hour), func(Event) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if got, err := tm.Load("future"); err != nil || !got.Enabled {
		t.Fatalf("future one-shot %+v %v", got, err)
	}
	inside := at.Add(26 * time.Hour)
	if err := tm.Save(Routine{ID: "inside", Name: "i", Prompt: "p", At: inside.Format(time.RFC3339), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	posted = nil
	if err := tm.Fire(inside.Add(10*time.Second), func(ev Event) error {
		posted = append(posted, ev)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(posted) != 1 {
		t.Fatalf("inside window %+v", posted)
	}
	// A poll 90 seconds late is still inside the two-minute window.
	late := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	if err := tm.Save(Routine{ID: "late", Name: "l", Prompt: "p", At: late.Format(time.RFC3339), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	posted = nil
	if err := tm.Fire(late.Add(90*time.Second), func(ev Event) error {
		posted = append(posted, ev)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(posted) != 1 {
		t.Fatalf("90s late one-shot should fire: %+v", posted)
	}
}
