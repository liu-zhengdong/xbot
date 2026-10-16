package main

import (
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
)

// TestOpenKernelStepsFollowTheRole is the regression the serve path needs: it
// opens a kernel exactly the way cmd_serve does, with steps 0, so the bot's
// role decides the cap. A dev bot must get 24 steps, a chat bot 8.
func TestOpenKernelStepsFollowTheRole(t *testing.T) {
	t.Setenv("XBOT_API_KEY", "sk-test")
	for _, c := range []struct {
		role string
		want int
	}{{role: "", want: 8}, {role: bot.RoleChat, want: 8}, {role: bot.RoleDev, want: 24}} {
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
		if k.MaxSteps != c.want {
			t.Fatalf("role %q: openKernel with steps 0 gave MaxSteps %d, want %d", c.role, k.MaxSteps, c.want)
		}
	}

	// An explicit step count still wins over the role.
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "persona", []string{link.NameTerminal})
	if err != nil {
		t.Fatal(err)
	}
	role := bot.RoleDev
	if err := b.ApplyProfile(bot.ProfileUpdate{Role: &role}); err != nil {
		t.Fatal(err)
	}
	k, _, err := openKernel(root, b, 3, nil, nil, false, runnerSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if k.MaxSteps != 3 {
		t.Fatalf("explicit steps: MaxSteps %d, want 3", k.MaxSteps)
	}
}
