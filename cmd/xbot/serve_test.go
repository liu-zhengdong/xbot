package main

import (
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
)

// The main line uses the runaway guard; subagent types own their budgets.
func TestOpenKernelKeepsDefaultStepGuard(t *testing.T) {
	t.Setenv("XBOT_API_KEY", "sk-test")
	b, err := bot.Create(t.TempDir(), "ada", "persona", []string{link.NameTerminal})
	if err != nil {
		t.Fatal(err)
	}
	k, err := newServeKernel(b.Root, b)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	if k.MaxSteps != 0 {
		t.Fatalf("serve pinned MaxSteps %d; it must use the default guard", k.MaxSteps)
	}
}
