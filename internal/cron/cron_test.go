package cron

import (
	"testing"
	"time"
)

func TestMatchAndMinGap(t *testing.T) {
	s, err := Parse("*/5 * * * *")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Match(time.Date(2026, 10, 15, 8, 10, 30, 0, time.UTC)) {
		t.Fatal("expected match at minute 10")
	}
	if s.Match(time.Date(2026, 10, 15, 8, 11, 0, 0, time.UTC)) {
		t.Fatal("minute 11 should not match")
	}
	gap, err := s.MinGap(20)
	if err != nil || gap != 5*time.Minute {
		t.Fatalf("gap %s err %v", gap, err)
	}
	if err := Allow("*/5 * * * *"); err != nil {
		t.Fatal(err)
	}
	if err := Allow("* * * * *"); err == nil {
		t.Fatal("every minute must be rejected")
	}
	if err := Allow("0 * * * *"); err != nil {
		t.Fatal(err)
	}
	if err := Allow("7 * * * *"); err != nil {
		t.Fatal(err)
	}
}

func TestDayOfMonthOrWeek(t *testing.T) {
	s, err := Parse("0 0 1 * 1")
	if err != nil {
		t.Fatal(err)
	}
	monday := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC) // Monday
	if !s.Match(monday) {
		t.Fatal("Monday should match when both dom and dow are set")
	}
	first := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC) // Thursday
	if !s.Match(first) {
		t.Fatal("the 1st should match")
	}
	other := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	if s.Match(other) {
		t.Fatal("Tuesday the 6th should not match")
	}
}

func TestSundaySeven(t *testing.T) {
	s, err := Parse("0 0 * * 7")
	if err != nil {
		t.Fatal(err)
	}
	sunday := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	if !s.Match(sunday) {
		t.Fatal("7 is Sunday")
	}
}
