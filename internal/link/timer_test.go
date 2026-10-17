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

// A fixed wall time inside the repeated fall-back hour fires once, not twice.
func TestDSTRepeatedHourFiresOnce(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	tm := &Timer{Dir: filepath.Join(t.TempDir(), "routines"), Loc: loc}
	// Two times inside the repeated hour: each wall time fires once.
	if err := tm.Save(Routine{ID: "night", Name: "n", Prompt: "p", Cron: "0,30 1 * * *", Zone: "local", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	fire := func(when time.Time) int {
		t.Helper()
		var posted []Event
		if err := tm.Fire(when, func(ev Event) error {
			posted = append(posted, ev)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return len(posted)
	}
	// 2026-11-01: 01:00/01:30 EDT are 05:00Z/05:30Z; 01:00/01:30 EST are 06:00Z/06:30Z.
	for _, c := range []struct {
		h, m, want int
	}{{5, 0, 1}, {5, 30, 1}, {6, 0, 0}, {6, 30, 0}} {
		if n := fire(time.Date(2026, 11, 1, c.h, c.m, 0, 0, time.UTC)); n != c.want {
			t.Fatalf("%02d:%02dZ fired %d, want %d", c.h, c.m, n, c.want)
		}
	}
	if n := fire(time.Date(2026, 11, 2, 6, 30, 0, 0, time.UTC)); n != 1 {
		t.Fatalf("next day fired %d", n)
	}
}

// A wall time skipped by spring-forward does not fire.
func TestDSTSkippedHourDoesNotFire(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	tm := &Timer{Dir: filepath.Join(t.TempDir(), "routines"), Loc: loc}
	if err := tm.Save(Routine{ID: "gap", Name: "g", Prompt: "p", Cron: "30 2 * * *", Zone: "local", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	for _, when := range []time.Time{
		time.Date(2026, 3, 8, 6, 30, 0, 0, time.UTC), // 01:30 EST
		time.Date(2026, 3, 8, 7, 30, 0, 0, time.UTC), // 03:30 EDT
	} {
		var posted []Event
		if err := tm.Fire(when, func(ev Event) error {
			posted = append(posted, ev)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if len(posted) != 0 {
			t.Fatalf("%s fired %+v", when, posted)
		}
	}
}

// An hourly wildcard still fires every hour across both transitions.
func TestDSTHourlyWildcardFiresEveryHour(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	tm := &Timer{Dir: filepath.Join(t.TempDir(), "routines"), Loc: loc}
	if err := tm.Save(Routine{ID: "hourly", Name: "h", Prompt: "p", Cron: "0 * * * *", Zone: "local", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	check := func(start time.Time, n int) {
		t.Helper()
		for i := 0; i < n; i++ {
			when := start.Add(time.Duration(i) * time.Hour)
			var posted []Event
			if err := tm.Fire(when, func(ev Event) error {
				posted = append(posted, ev)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if len(posted) != 1 {
				t.Fatalf("%s: %d events", when, len(posted))
			}
		}
	}
	// Fall back: 01:00 EDT, 01:00 EST, 02:00 EST.
	check(time.Date(2026, 11, 1, 5, 0, 0, 0, time.UTC), 3)
	// Spring forward: 01:00 EST, 03:00 EDT, 04:00 EDT.
	check(time.Date(2026, 3, 8, 6, 0, 0, 0, time.UTC), 3)
}
