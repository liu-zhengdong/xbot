package bot

import (
	"strings"
	"testing"
	"time"
)

// TestDefaultsForRole pins the three role buckets: chat is the interactive
// default, dev trades longer turns for engineering work.
func TestDefaultsForRole(t *testing.T) {
	cases := []struct {
		role  string
		steps int
		tools int
		dur   time.Duration
	}{
		{role: "", steps: 8, tools: 4, dur: 20 * time.Second},
		{role: RoleChat, steps: 8, tools: 4, dur: 20 * time.Second},
		{role: RoleDev, steps: 24, tools: 8, dur: 60 * time.Second},
		{role: "unknown", steps: 8, tools: 4, dur: 20 * time.Second},
	}
	for _, c := range cases {
		got := DefaultsForRole(c.role)
		if got.MaxSteps != c.steps || got.ForegroundMaxTools != c.tools || got.ForegroundMaxTime != c.dur {
			t.Errorf("DefaultsForRole(%q) = %+v, want %d/%d/%s", c.role, got, c.steps, c.tools, c.dur)
		}
	}
}

// TestConfigWorkingOverrides checks an explicit override wins over the role
// default while the other values still come from the role.
func TestConfigWorkingOverrides(t *testing.T) {
	dev := Config{Role: RoleDev}.Working()
	if dev.MaxSteps != 24 || dev.ForegroundMaxTools != 8 || dev.ForegroundMaxTime != 60*time.Second {
		t.Fatalf("dev defaults %+v", dev)
	}
	mixed := Config{Role: RoleDev, MaxSteps: 3}.Working()
	if mixed.MaxSteps != 3 || mixed.ForegroundMaxTools != 8 || mixed.ForegroundMaxTime != 60*time.Second {
		t.Fatalf("dev with max_steps override %+v", mixed)
	}
	tools := Config{Role: RoleDev, ForegroundMaxTools: 2}.Working()
	if tools.MaxSteps != 24 || tools.ForegroundMaxTools != 2 {
		t.Fatalf("dev with foreground tool override %+v", tools)
	}
	secs := Config{Role: RoleDev, ForegroundMaxTime: 10}.Working()
	if secs.ForegroundMaxTime != 10*time.Second {
		t.Fatalf("dev with foreground time override %+v", secs)
	}
	// A negative value turns that limit off and is passed through untouched.
	off := Config{Role: RoleChat, ForegroundMaxTools: -1}.Working()
	if off.ForegroundMaxTools != -1 {
		t.Fatalf("negative override was rewritten: %+v", off)
	}
}

// TestApplyProfileRoleAndSteps checks the new profile fields are validated,
// stored, and that a bad value leaves the stored config alone.
func TestApplyProfileRoleAndSteps(t *testing.T) {
	b, err := Create(t.TempDir(), "ada", "Be brief.", nil)
	if err != nil {
		t.Fatal(err)
	}

	role := RoleDev
	steps := 12
	tools := 6
	secs := 45
	if err := b.ApplyProfile(ProfileUpdate{Role: &role, MaxSteps: &steps, ForegroundMaxTools: &tools, ForegroundMaxTime: &secs}); err != nil {
		t.Fatal(err)
	}
	cfg := b.Config()
	if cfg.Role != RoleDev || cfg.MaxSteps != 12 || cfg.ForegroundMaxTools != 6 || cfg.ForegroundMaxTime != 45 {
		t.Fatalf("stored config %+v", cfg)
	}
	if w := cfg.Working(); w.MaxSteps != 12 || w.ForegroundMaxTools != 6 || w.ForegroundMaxTime != 45*time.Second {
		t.Fatalf("working %+v", w)
	}

	bad := ProfileUpdate{Role: strptr("wizard")}
	if err := b.ApplyProfile(bad); err == nil || !strings.Contains(err.Error(), "role") {
		t.Fatalf("bad role accepted: %v", err)
	}
	for _, n := range []int{0, 65} {
		if err := b.ApplyProfile(ProfileUpdate{MaxSteps: &n}); err == nil {
			t.Fatalf("max_steps %d accepted", n)
		}
	}
	if err := b.ApplyProfile(ProfileUpdate{ForegroundMaxTime: intptr(601)}); err == nil {
		t.Fatal("foreground_max_time 601 accepted")
	}
	if got := b.Config(); got.Role != RoleDev || got.MaxSteps != 12 {
		t.Fatalf("a rejected update changed the config: %+v", got)
	}
}

func strptr(s string) *string { return &s }
func intptr(n int) *int       { return &n }
