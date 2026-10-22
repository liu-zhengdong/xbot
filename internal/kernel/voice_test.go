package kernel

import (
	"context"
	"strings"
	"testing"
	"time"

	"xbot/internal/exec"
	"xbot/internal/link"
	"xbot/internal/provider"
)

func TestVoiceAppliesToExistingCustomBot(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.WritePersona("A custom expert."); err != nil {
		t.Fatal(err)
	}
	if err := b.WriteRules("Saved custom rules.\n" + legacyVerifyRule); err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "好。"}, {Role: provider.RoleAssistant, Content: "好。"}}}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 2}
	for _, text := range []string{"hi", "again"} {
		if err := k.Handle(context.Background(), link.TerminalEvent("u1", text, time.Now())); err != nil {
			t.Fatal(err)
		}
	}
	first := p.reqs[0].Messages[0].Content
	if !strings.Contains(first, VoiceRule) || !strings.Contains(first, VerifyRule) || strings.Contains(first, legacyVerifyRule) || !strings.Contains(first, "A custom expert.") {
		t.Fatal(first)
	}
	if len(strings.Split(VoiceRule, "\n")) > 8 {
		t.Fatal("voice rule grew beyond 8 lines")
	}
	if p.reqs[1].Messages[0].Content != first {
		t.Fatal("voice rules changed the stable prefix between turns")
	}
}

func TestUnboundDeepSeekUsesActualOverlay(t *testing.T) {
	b := testBot(t)
	for _, id := range []string{"deepseek-flash", "deepseek-chat", "group/auto-deepseek-v4-1-flash", "deepseek-v4.1-flash"} {
		k := &Kernel{Bot: b, Provider: &provider.Client{Model: id}}
		key, text, err := k.modelOverlay()
		if err != nil || key != "deepseek/"+id || !strings.Contains(text, "Act once") {
			t.Fatalf("%s: %s %q %v", id, key, text, err)
		}
	}
	k := &Kernel{Bot: b, Provider: &provider.Client{Model: "gpt-5"}}
	_, text, err := k.modelOverlay()
	if err != nil || text != "" {
		t.Fatalf("strong model got DeepSeek overlay: %q %v", text, err)
	}
}

func TestSubagentReportsReceiveInternalRetellHint(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "结果到了。"}}}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 2}
	ev := link.NewEvent(link.SourceSubagent, exec.EvDone, "w1 did the work", "sa_a", time.Now())
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range p.reqs[0].Messages {
		if strings.Contains(m.Content, "<subagent_report>") {
			found = true
		}
	}
	if !found {
		t.Fatal("retell hint did not reach the main model")
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Message.Content, "<subagent_report>") && !e.IsInternal() {
			t.Fatal("hint exposed to owner")
		}
	}
	// Absorbed reports use the same internal hint; ordinary events do not.
	hint, ok := subagentReportHint(ev)
	if !ok || !hint.Internal {
		t.Fatal(hint)
	}
	if _, ok := subagentReportHint(link.TerminalEvent("u1", "hi", time.Now())); ok {
		t.Fatal("owner message got a report hint")
	}
}

type voiceExec struct {
	exec.Executor
	req exec.StartRequest
}

func (e *voiceExec) Start(_ context.Context, req exec.StartRequest) (exec.Task, error) {
	e.req = req
	return exec.Task{ID: req.ID, Task: req.Task, Status: "running"}, nil
}
func TestBriefWorkersReceiveModelSupplement(t *testing.T) {
	b := testBot(t)
	if err := b.SetBinding("deepseek", "group/auto-deepseek-v4-1-flash"); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b}
	sink := &voiceExec{}
	e := modelExecutor{Executor: sink, k: k}
	for _, mode := range []string{exec.ModeBrief, exec.ModeFork} {
		if _, err := e.Start(context.Background(), exec.StartRequest{Mode: mode, Persona: "worker"}); err != nil {
			t.Fatal(err)
		}
		has := strings.Contains(sink.req.Persona, "Act once")
		if has != (mode == exec.ModeBrief) {
			t.Fatalf("mode %s: %q", mode, sink.req.Persona)
		}
	}
}

func (e *voiceExec) Tasks() ([]exec.Task, error) { return nil, nil }
