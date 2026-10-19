package bot

import (
	"strings"
	"testing"
	"time"
)

// TestDefaultsForRole pins the two role buckets: chat is the interactive
// default, dev trades longer turns for engineering work. Steps are not part of
// it: what bounds a background is its wall-clock budget.
func TestDefaultsForRole(t *testing.T) {
	cases := []struct {
		role  string
		tools int
		dur   time.Duration
		bg    time.Duration
	}{
		{role: "", tools: 4, dur: 20 * time.Second, bg: 45 * time.Minute},
		{role: RoleChat, tools: 4, dur: 20 * time.Second, bg: 45 * time.Minute},
		{role: RoleDev, tools: 8, dur: 60 * time.Second, bg: 2 * time.Hour},
		{role: "unknown", tools: 4, dur: 20 * time.Second, bg: 45 * time.Minute},
	}
	for _, c := range cases {
		got := DefaultsForRole(c.role)
		if got.ForegroundMaxTools != c.tools || got.ForegroundMaxTime != c.dur || got.BackgroundMaxTime != c.bg {
			t.Errorf("DefaultsForRole(%q) = %+v, want %d/%s/%s", c.role, got, c.tools, c.dur, c.bg)
		}
	}
}

// TestConfigWorkingOverrides checks an explicit override wins over the role
// default while the other values still come from the role.
func TestConfigWorkingOverrides(t *testing.T) {
	dev := Config{Role: RoleDev}.Working()
	if dev.ForegroundMaxTools != 8 || dev.ForegroundMaxTime != 60*time.Second || dev.BackgroundMaxTime != 2*time.Hour {
		t.Fatalf("dev defaults %+v", dev)
	}
	tools := Config{Role: RoleDev, ForegroundMaxTools: 2}.Working()
	if tools.ForegroundMaxTools != 2 || tools.BackgroundMaxTime != 2*time.Hour {
		t.Fatalf("dev with foreground tool override %+v", tools)
	}
	secs := Config{Role: RoleDev, ForegroundMaxTime: 10}.Working()
	if secs.ForegroundMaxTime != 10*time.Second {
		t.Fatalf("dev with foreground time override %+v", secs)
	}
	// The background budget is what a background is bounded by, so it is the one
	// override worth setting: seconds in, a duration out.
	bg := Config{Role: RoleChat, BackgroundMaxTime: 90 * 60}.Working()
	if bg.BackgroundMaxTime != 90*time.Minute || bg.ForegroundMaxTime != 20*time.Second {
		t.Fatalf("background override %+v", bg)
	}
	// A negative value turns that limit off. Durations keep their sign; the
	// callers test for "less than zero", so seconds become negative seconds.
	off := Config{Role: RoleChat, ForegroundMaxTools: -1, BackgroundMaxTime: -1}.Working()
	if off.ForegroundMaxTools != -1 || off.BackgroundMaxTime >= 0 {
		t.Fatalf("negative override was rewritten: %+v", off)
	}
}

// TestApplyProfileRoleAndBudgets checks the profile fields are validated,
// stored, and that a bad value leaves the stored config alone.
func TestApplyProfileRoleAndBudgets(t *testing.T) {
	b, err := Create(t.TempDir(), "ada", "Be brief.", nil)
	if err != nil {
		t.Fatal(err)
	}

	role := RoleDev
	tools := 6
	secs := 45
	bg := 90 * 60
	if err := b.ApplyProfile(ProfileUpdate{Role: &role, ForegroundMaxTools: &tools, ForegroundMaxTime: &secs, BackgroundMaxTime: &bg}); err != nil {
		t.Fatal(err)
	}
	cfg := b.Config()
	if cfg.Role != RoleDev || cfg.ForegroundMaxTools != 6 || cfg.ForegroundMaxTime != 45 || cfg.BackgroundMaxTime != 90*60 {
		t.Fatalf("stored config %+v", cfg)
	}
	if w := cfg.Working(); w.ForegroundMaxTools != 6 || w.ForegroundMaxTime != 45*time.Second || w.BackgroundMaxTime != 90*time.Minute {
		t.Fatalf("working %+v", w)
	}

	bad := ProfileUpdate{Role: strptr("wizard")}
	if err := b.ApplyProfile(bad); err == nil || !strings.Contains(err.Error(), "role") {
		t.Fatalf("bad role accepted: %v", err)
	}
	if err := b.ApplyProfile(ProfileUpdate{ForegroundMaxTime: intptr(601)}); err == nil {
		t.Fatal("foreground_max_time 601 accepted")
	}
	if err := b.ApplyProfile(ProfileUpdate{BackgroundMaxTime: intptr(24*3600 + 1)}); err == nil {
		t.Fatal("background_max_time past a day accepted")
	}
	if got := b.Config(); got.Role != RoleDev || got.ForegroundMaxTools != 6 {
		t.Fatalf("a rejected update changed the config: %+v", got)
	}
}

func strptr(s string) *string { return &s }
func intptr(n int) *int       { return &n }
