package main

import (
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
)

// TestOpenKernelBudgetFollowsTheRole is the regression the serve path needs: it
// opens a kernel exactly the way cmd_serve does, with no step count, so the
// bot's role decides how long a background may work. Steps are only the
// runaway guard.
func TestOpenKernelBudgetFollowsTheRole(t *testing.T) {
	t.Setenv("XBOT_API_KEY", "sk-test")
	for _, c := range []struct {
		role string
		want time.Duration
	}{
		{role: "", want: 45 * time.Minute},
		{role: bot.RoleChat, want: 45 * time.Minute},
		{role: bot.RoleDev, want: 2 * time.Hour},
	} {
		root := t.TempDir()
		b, err := bot.Create(root, "ada", "persona", []string{link.NameTerminal})
		if err != nil {
			t.Fatal(err)
		}
		if c.role != "" {
			role := c.role
			if err := b.ApplyProfile(bot.ProfileUpdate{Role: &role}); err != nil {
				t.Fatal(err)
			}
		}
		k, err := newServeKernel(root, b)
		if err != nil {
			t.Fatal(err)
		}
		if k.BackgroundMaxTime != c.want {
			t.Fatalf("role %q: openKernel with steps 0 gave BackgroundMaxTime %s, want %s", c.role, k.BackgroundMaxTime, c.want)
		}
		if k.MaxSteps != 0 {
			t.Fatalf("role %q: serve pinned MaxSteps %d; it must stay 0 so the guard applies", c.role, k.MaxSteps)
		}
	}

	// The profile override wins over the role.
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "persona", []string{link.NameTerminal})
	if err != nil {
		t.Fatal(err)
	}
	seconds := 15 * 60
	if err := b.ApplyProfile(bot.ProfileUpdate{BackgroundMaxTime: &seconds}); err != nil {
		t.Fatal(err)
	}
	k, err := newServeKernel(root, b)
	if err != nil {
		t.Fatal(err)
	}
	if k.BackgroundMaxTime != 15*time.Minute {
		t.Fatalf("background override: BackgroundMaxTime %s, want 15m", k.BackgroundMaxTime)
	}
}
