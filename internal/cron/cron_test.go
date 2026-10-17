package cron

import (
	"testing"
	"time"
	_ "time/tzdata"
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

func TestConvertFixedZones(t *testing.T) {
	shanghai := time.FixedZone("CST", 8*3600)
	kolkata := time.FixedZone("IST", 5*3600+30*60)
	start := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		expr string
		loc  *time.Location
		want string
		ok   bool
	}{
		{"0 1,3,5,7,9 * * *", shanghai, "0 9,11,13,15,17 * * *", true},
		{"30 0 * * *", shanghai, "30 8 * * *", true},
		{"0 13 * * *", shanghai, "0 21 * * *", true},
		{"0 20 * * 1", shanghai, "0 4 * * 2", true},
		{"0 4 1 * *", shanghai, "0 12 1 * *", true},
		{"0 20 1 * *", shanghai, "", false},
		{"*/15 * * * *", shanghai, "*/15 * * * *", true},
		{"0 1 * * *", kolkata, "30 6 * * *", true},
	}
	for _, c := range cases {
		got, ok, err := Convert(c.expr, time.UTC, c.loc, start)
		if err != nil {
			t.Fatalf("%s: %v", c.expr, err)
		}
		if ok != c.ok || (ok && got != c.want) {
			t.Fatalf("%s -> %q ok=%v, want %q ok=%v", c.expr, got, ok, c.want, c.ok)
		}
	}
}

func TestConvertDSTZoneFallsBack(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	if _, ok, err := Convert("0 12 * * *", time.UTC, ny, start); err != nil || ok {
		t.Fatalf("DST conversion ok=%v err=%v", ok, err)
	}
}

func TestConvertLondonSameOffsetStillVerifies(t *testing.T) {
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC) // GMT, offset 0 like UTC
	if _, ok, err := Convert("0 9 * * *", time.UTC, london, start); err != nil || ok {
		t.Fatalf("daily 09:00 must not convert to a DST zone: ok=%v err=%v", ok, err)
	}
	got, ok, err := Convert("*/15 * * * *", time.UTC, london, start)
	if err != nil || !ok || got != "*/15 * * * *" {
		t.Fatalf("quarter-hourly: %q ok=%v err=%v", got, ok, err)
	}
}

func TestConvertNegativeOffset(t *testing.T) {
	west := time.FixedZone("EST", -5*3600)
	start := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	got, ok, err := Convert("0 3 * * 2", time.UTC, west, start)
	if err != nil || !ok || got != "0 22 * * 1" {
		t.Fatalf("monday 22:00 local: %q ok=%v err=%v", got, ok, err)
	}
}

func TestConvertMixedCarryFallsBack(t *testing.T) {
	shanghai := time.FixedZone("CST", 8*3600)
	start := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	if _, ok, err := Convert("0 10,20 * * 1", time.UTC, shanghai, start); err != nil || ok {
		t.Fatalf("mixed weekday carry: ok=%v err=%v", ok, err)
	}
}

func TestMatchInAndNextFixedZone(t *testing.T) {
	shanghai := time.FixedZone("CST", 8*3600)
	s, err := Parse("0 9 * * *")
	if err != nil {
		t.Fatal(err)
	}
	hit := time.Date(2026, 10, 18, 1, 0, 0, 0, time.UTC)
	if !s.MatchIn(hit, shanghai) {
		t.Fatal("09:00 +08 is 01:00Z")
	}
	after := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	next, ok := s.Next(after, shanghai)
	if !ok || !next.Equal(hit) {
		t.Fatalf("next %s ok=%v, want %s", next, ok, hit)
	}
}
