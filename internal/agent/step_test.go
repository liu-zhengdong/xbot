package agent

import (
	"context"
	"testing"

	"xbot/internal/provider"
	"xbot/internal/tools"
)

// TestOnStepCountsEveryModelCall pins the hook the kernel uses to count steps
// across several runs of one background task. The closing summary is not a step.
func TestOnStepCountsEveryModelCall(t *testing.T) {
	p := &alwaysTools{}
	steps := 0
	if _, err := Run(context.Background(), p, []tools.Tool{&echoTool{}}, nil, Options{
		MaxSteps: 2,
		OnStep:   func() { steps++ },
	}); err != nil {
		t.Fatal(err)
	}
	if steps != 2 {
		t.Fatalf("OnStep ran %d times, want one per step (2)", steps)
	}
	if len(p.reqs) != 3 {
		t.Fatalf("model calls %d, want 2 steps plus the summary", len(p.reqs))
	}

	sp := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "done"}}}
	steps = 0
	if _, err := Run(context.Background(), sp, nil, nil, Options{MaxSteps: 4, OnStep: func() { steps++ }}); err != nil {
		t.Fatal(err)
	}
	if steps != 1 {
		t.Fatalf("OnStep ran %d times for a single answer, want 1", steps)
	}
}

// TestOnCapMarksTheStepLimit checks the cap hook fires only when the run really
// used every step, and that it does not change what Run reports.
func TestOnCapMarksTheStepLimit(t *testing.T) {
	p := &alwaysTools{}
	capped := 0
	got, err := Run(context.Background(), p, []tools.Tool{&echoTool{}}, nil, Options{
		MaxSteps: 2,
		OnCap:    func() { capped++ },
	})
	if err != nil {
		t.Fatal(err)
	}
	if capped != 1 {
		t.Fatalf("OnCap ran %d times at the step limit, want 1", capped)
	}
	if last := got[len(got)-1]; last.Role != provider.RoleAssistant || len(last.ToolCalls) != 0 {
		t.Fatalf("the closing summary changed: %+v", last)
	}

	// A single step is all the room there is, so the cap is reached too.
	capped = 0
	if _, err := Run(context.Background(), &alwaysTools{}, []tools.Tool{&echoTool{}}, nil, Options{
		MaxSteps: 1,
		OnCap:    func() { capped++ },
	}); err != nil {
		t.Fatal(err)
	}
	if capped != 1 {
		t.Fatalf("OnCap ran %d times, want 1", capped)
	}

	// A plain answer never reaches the cap.
	sp := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "done"}}}
	capped = 0
	if _, err := Run(context.Background(), sp, nil, nil, Options{MaxSteps: 4, OnCap: func() { capped++ }}); err != nil {
		t.Fatal(err)
	}
	if capped != 0 {
		t.Fatalf("OnCap ran %d times for an answer, want 0", capped)
	}
}

// TestOnCapNotRunOnProviderError checks a failing model call is not reported as
// hitting the step cap: the two mean different things to the caller.
func TestOnCapNotRunOnProviderError(t *testing.T) {
	capped := 0
	_, err := Run(context.Background(), &scripted{}, nil, nil, Options{MaxSteps: 2, OnCap: func() { capped++ }})
	if err == nil {
		t.Fatal("a provider error should surface")
	}
	if capped != 0 {
		t.Fatalf("OnCap ran %d times on an error, want 0", capped)
	}
}
