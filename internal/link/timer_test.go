package link

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRoutineMinIntervalAndDueOnce(t *testing.T) {
	tm := &Timer{Dir: filepath.Join(t.TempDir(), "routines"), Sender: "u1"}
	err := tm.Save(Routine{ID: "often", Name: "often", Prompt: "ping", Cron: "* * * * *", Enabled: true})
	if err == nil {
		t.Fatal("every minute must be rejected")
	}
	if err := tm.Save(Routine{ID: "hourly", Name: "Hourly", Prompt: "check mail", Cron: "7 * * * *", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 15, 8, 7, 30, 0, time.UTC)
	var posted []Event
	if err := tm.Fire(now, func(ev Event) error {
		posted = append(posted, ev)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(posted) != 1 || posted[0].Source != NameTimer || posted[0].Sender != "u1" {
		t.Fatalf("%+v", posted)
	}
	if !posted[0].Untrusted {
		t.Fatal("routine events are untrusted")
	}
	posted = nil
	if err := tm.Fire(now, func(ev Event) error {
		posted = append(posted, ev)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(posted) != 0 {
		t.Fatalf("same slot fired again: %+v", posted)
	}
	later := now.Add(time.Hour)
	if err := tm.Fire(later, func(ev Event) error {
		posted = append(posted, ev)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(posted) != 1 {
		t.Fatalf("next hour %+v", posted)
	}
}
