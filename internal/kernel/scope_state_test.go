package kernel

import (
	"testing"
	"time"

	"xbot/internal/exec"
	"xbot/internal/link"
	"xbot/internal/provider"
)

// A background branch's call adds to the session totals but does not replace
// the foreground's latest prompt count, which the 50/75/90% reminder and the
// status line read (#20).
func TestBackgroundUsageKeepsForegroundCount(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, Now: func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }}
	call := func(tr *turn, prompt int) {
		k.noteUsage(tr, provider.Request{}, provider.Message{
			Role:  provider.RoleAssistant,
			Usage: provider.TokenUsage{PromptTokens: prompt, CompletionTokens: 10, Reported: true},
		})
	}
	call(&turn{}, 90_000)
	call(&turn{branch: "bg_1"}, 1_000)
	snap, ok := k.tokenSnap()
	if !ok || snap.Prompt != 90_000 {
		t.Fatalf("foreground prompt %+v", snap)
	}
	if snap.SessionPrompt != 91_000 || snap.SessionCompletion != 20 {
		t.Fatalf("session totals %+v", snap)
	}
}

// Each background branch and subagent has its own cwd entry; it is dropped
// when that branch or subagent ends (#22).
func TestEndedScopesDropTheirCwd(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, Now: func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }}
	dir := t.TempDir()
	for _, scope := range []string{"bg_1", "sa_1", "sa_2"} {
		k.noteScope(scope, dir)
	}
	k.finishBackground(&turn{branch: "bg_1", task: "t"}, nil, nil)
	ch := make(chan exec.Event, 2)
	ch <- exec.Event{AgentID: "sa_1", Kind: exec.EvDone}
	ch <- exec.Event{AgentID: "sa_2", Kind: exec.EvProgress}
	close(ch)
	k.forwardExec(ch)
	for scope, want := range map[string]string{"bg_1": "", "sa_1": "", "sa_2": dir} {
		if got := k.readScope(scope); got != want {
			t.Fatalf("scope %s: %q, want %q", scope, got, want)
		}
	}
}
