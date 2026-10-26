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
		role string
		bg   time.Duration
	}{
		{role: "", bg: 45 * time.Minute},
		{role: RoleChat, bg: 45 * time.Minute},
		{role: RoleDev, bg: 2 * time.Hour},
		{role: "unknown", bg: 45 * time.Minute},
	}
	for _, c := range cases {
		got := DefaultsForRole(c.role)
		if got.BackgroundMaxTime != c.bg {
			t.Errorf("DefaultsForRole(%q) = %+v, want %s", c.role, got, c.bg)
		}
	}
}

// TestConfigWorkingOverrides checks an explicit override wins over the role
// default while the other values still come from the role.
func TestConfigWorkingOverrides(t *testing.T) {
	dev := Config{Role: RoleDev}.Working()
	if dev.BackgroundMaxTime != 2*time.Hour {
		t.Fatalf("dev defaults %+v", dev)
	}
	// The background budget is what a background is bounded by, so it is the one
	// override worth setting: seconds in, a duration out.
	bg := Config{Role: RoleChat, BackgroundMaxTime: 90 * 60}.Working()
	if bg.BackgroundMaxTime != 90*time.Minute {
		t.Fatalf("background override %+v", bg)
	}
	// A negative value turns that limit off. Durations keep their sign; the
	// callers test for "less than zero", so seconds become negative seconds.
	off := Config{Role: RoleChat, BackgroundMaxTime: -1}.Working()
	if off.BackgroundMaxTime >= 0 {
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
	bg := 90 * 60
	if err := b.ApplyProfile(ProfileUpdate{Role: &role, BackgroundMaxTime: &bg}); err != nil {
		t.Fatal(err)
	}
	cfg := b.Config()
	if cfg.Role != RoleDev || cfg.BackgroundMaxTime != 90*60 {
		t.Fatalf("stored config %+v", cfg)
	}
	if w := cfg.Working(); w.BackgroundMaxTime != 90*time.Minute {
		t.Fatalf("working %+v", w)
	}

	bad := ProfileUpdate{Role: strptr("wizard")}
	if err := b.ApplyProfile(bad); err == nil || !strings.Contains(err.Error(), "role") {
		t.Fatalf("bad role accepted: %v", err)
	}
	if err := b.ApplyProfile(ProfileUpdate{BackgroundMaxTime: intptr(24*3600 + 1)}); err == nil {
		t.Fatal("background_max_time past a day accepted")
	}
	// Negative turns the budget off. The CLI multiplies minutes by 60, so -1 on
	// the command line arrives as -60: any negative value means "no limit".
	if err := b.ApplyProfile(ProfileUpdate{BackgroundMaxTime: intptr(-60)}); err != nil {
		t.Fatalf("a negative background_max_time was rejected: %v", err)
	}
	if got := b.Config().BackgroundMaxTime; got != -1 {
		t.Fatalf("stored background_max_time %d, want the off marker -1", got)
	}
	if w := b.Config().Working().BackgroundMaxTime; !(w < 0) {
		t.Fatalf("a negative budget did not reach the working defaults: %s", w)
	}
	if got := b.Config(); got.Role != RoleDev || got.BackgroundMaxTime != -1 {
		t.Fatalf("a rejected update changed the config: %+v", got)
	}
}

func strptr(s string) *string { return &s }
func intptr(n int) *int       { return &n }
