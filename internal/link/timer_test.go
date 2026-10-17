package link

import (
	"path/filepath"
	"testing"
	"time"
	_ "time/tzdata"
)

func TestOneShotFiresOnceNearTheSlot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "routines")
	tm := &Timer{Dir: dir, Sender: "u1"}
	at := time.Now().UTC().Add(2 * time.Minute).Truncate(time.Second)
	kAt := at.Format(time.RFC3339)
	if err := tm.Save(Routine{ID: "sip", Name: "Sip", Prompt: "drink", At: kAt, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	var posted []Event
	if err := tm.Fire(at.Add(-20*time.Second), func(ev Event) error {
		posted = append(posted, ev)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(posted) != 0 {
		t.Fatalf("early %+v", posted)
	}
	if err := tm.Fire(at.Add(10*time.Second), func(ev Event) error {
		posted = append(posted, ev)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(posted) != 1 {
		t.Fatalf("slot %+v", posted)
	}
	if posted[0].Time.Sub(at) > 15*time.Second || at.Sub(posted[0].Time) > 15*time.Second {
		t.Fatalf("fire %s slot %s", posted[0].Time, at)
	}
	got, err := tm.Load("sip")
	if err != nil || got.Enabled {
		t.Fatalf("still enabled %+v %v", got, err)
	}
	posted = nil
	if err := tm.Fire(at.Add(10*time.Second), func(ev Event) error {
		posted = append(posted, ev)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(posted) != 0 {
		t.Fatalf("second fire %+v", posted)
	}
}

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
