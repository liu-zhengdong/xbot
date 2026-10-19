package kernel

import (
	"strings"
	"testing"
	"time"
)

// TestBackgroundBudgetIsTheOnlyTimeLimit pins the shape of the budget rule after
// the step-cap segments were removed: a background is bounded by wall-clock
// time, and nothing else starts another run.
func TestBackgroundBudgetIsTheOnlyTimeLimit(t *testing.T) {
	start := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	k := &Kernel{BackgroundMaxTime: 45 * time.Minute, Now: func() time.Time { return start }}

	// A turn still on the main line has no background budget: the foreground is
	// bounded by its own tool and time caps.
	if why, over := k.backgroundOverBudget(&turn{started: start.Add(-2 * time.Hour)}); over {
		t.Fatalf("a main-line turn was reported over the background budget: %s", why)
	}

	fresh := &turn{branch: "bg_a", started: start.Add(-10 * time.Minute)}
	if why, over := k.backgroundOverBudget(fresh); over {
		t.Fatalf("a background inside its budget was stopped: %s", why)
	}

	spent := &turn{branch: "bg_a", started: start.Add(-50 * time.Minute)}
	why, over := k.backgroundOverBudget(spent)
	if !over || !strings.Contains(why, "background_budget") {
		t.Fatalf("a background past its budget kept going: %v %q", over, why)
	}

	// A negative budget turns the limit off entirely.
	off := &Kernel{BackgroundMaxTime: -1, Now: func() time.Time { return start }}
	long := &turn{branch: "bg_a", started: start.Add(-24 * time.Hour)}
	if why, over := off.backgroundOverBudget(long); over {
		t.Fatalf("a negative budget still stopped the background: %s", why)
	}
}

// TestBackgroundBudgetDefault pins the fallback used when a kernel is built
// without a role budget.
func TestBackgroundBudgetDefault(t *testing.T) {
	k := &Kernel{}
	if got := k.backgroundBudget(); got != defaultBackgroundMaxTime {
		t.Fatalf("default budget %s", got)
	}
}
