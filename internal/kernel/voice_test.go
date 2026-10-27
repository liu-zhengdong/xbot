package kernel

import (
	"context"
	"os"
	"path/filepath"
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
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("好。")}, {Role: provider.RoleAssistant, Content: provider.TextContent("好。")}}}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 2}
	for _, text := range []string{"hi", "again"} {
		if err := k.Handle(context.Background(), link.TerminalEvent("u1", text, time.Now())); err != nil {
			t.Fatal(err)
		}
	}
	first := p.reqs[0].Messages[0].Text()
	if !strings.Contains(first, VoiceRule) || !strings.Contains(first, VerifyRule) || strings.Contains(first, legacyVerifyRule) || !strings.Contains(first, "A custom expert.") {
		t.Fatal(first)
	}
	if len(strings.Split(VoiceRule, "\n")) > 8 {
		t.Fatal("voice rule grew beyond 8 lines")
	}
	if p.reqs[1].Messages[0].Text() != first {
		t.Fatal("voice rules changed the stable prefix between turns")
	}
}

func TestDeepSeekBindingUsesItsOverlay(t *testing.T) {
	b := testBot(t)
	// Built-in overlays are empty; a local format supplement routes by the
	// bound registry entry and model, never by the client's model name.
	for _, id := range []string{"deepseek-flash", "deepseek-chat", "group/auto-deepseek-v4-1-flash", "deepseek-v4.1-flash"} {
		if err := b.SetBinding("deepseek", id); err != nil {
			t.Fatal(err)
		}
		k := &Kernel{Bot: b, Provider: &provider.Client{Model: "gpt-5"}}
		key, text, err := k.modelOverlay()
		if err != nil || key != "" || text != "" {
			t.Fatalf("embedded %s: %s %q %v", id, key, text, err)
		}
		writeTestOverlay(t, b.Root, id)
		key, text, err = k.modelOverlay()
		if err != nil || key != "deepseek/"+id || text != "Local format supplement." {
			t.Fatalf("%s: %s %q %v", id, key, text, err)
		}
	}
	if err := b.SetBinding("openai", "gpt-5"); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, Provider: &provider.Client{Model: "deepseek-flash"}}
	_, text, err := k.modelOverlay()
	if err != nil || text != "" {
		t.Fatalf("strong model got DeepSeek overlay: %q %v", text, err)
	}
}

func TestSubagentReportsUseNormalConversation(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameTools)
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("结果到了。")}, {Role: provider.RoleAssistant, Content: provider.TextContent("记得这个结果。")}}}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 2}
	ev := link.NewEvent(link.SourceSubagent, exec.EvDone, "A完成，验收值731010", "sa_a", time.Now())
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "继续", time.Now())); err != nil {
		t.Fatal(err)
	}
	for i, req := range p.reqs {
		if len(req.Tools) == 0 {
			t.Fatalf("request %d lost ordinary tools", i)
		}
		var all strings.Builder
		for _, msg := range req.Messages {
			all.WriteString(msg.Text())
		}
		if !strings.Contains(all.String(), ev.Content) || strings.Contains(all.String(), "<reported_result>") || strings.Contains(all.String(), "<subagent_report>") {
			t.Fatal(all.String())
		}
		if i == 1 && !strings.Contains(all.String(), "结果到了。") {
			t.Fatal("prior normal reply missing")
		}
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
	writeTestOverlay(t, b.Root, "group/auto-deepseek-v4-1-flash")
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
		has := strings.Contains(sink.req.Persona, "Local format supplement.")
		if has != (mode == exec.ModeBrief) {
			t.Fatalf("mode %s: %q", mode, sink.req.Persona)
		}
	}
}

func writeTestOverlay(t *testing.T, root, model string) {
	t.Helper()
	path := filepath.Join(root, "prompts", "models", "deepseek", model+".md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("Local format supplement."), 0600); err != nil {
		t.Fatal(err)
	}
}

func (e *voiceExec) Tasks() ([]exec.Task, error) { return nil, nil }
