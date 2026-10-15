package kernel

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

type pokeLink struct{ k *Kernel }

func (pokeLink) Name() string { return "poke" }
func (p pokeLink) Tools() []tools.Tool {
	return []tools.Tool{pokeTool{p.k}}
}

type pokeTool struct{ k *Kernel }

func (pokeTool) Name() string        { return "poke" }
func (pokeTool) Description() string { return "post an inbox event" }
func (pokeTool) Parameters() any     { return map[string]any{"type": "object"} }
func (pokeTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "poke", nil
}
func (p pokeTool) Run(context.Context, json.RawMessage) (string, error) {
	ev := link.NewEvent("webhook:peer", "push", "REPLY_LANDED", "bob", time.Unix(50, 0).UTC())
	if err := p.k.Inbox.Post(ev); err != nil {
		return "", err
	}
	return "posted", nil
}

func TestInboxEventArrivesBeforeTheNextStep(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "poke", Arguments: "{}"},
		}}},
		{Role: provider.RoleAssistant, Content: "saw it"},
	}}
	k := &Kernel{
		Bot: b, Provider: p, Inbox: box, Memory: memory.NopStore{}, MaxSteps: 4,
		Links: []link.Link{link.Terminal{}},
	}
	k.Links = append(k.Links, pokeLink{k})
	ev := link.TerminalEvent("u1", "ask bob", time.Unix(10, 0).UTC())
	if err := box.Post(ev); err != nil {
		t.Fatal(err)
	}
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) < 2 {
		t.Fatalf("steps %d", len(p.reqs))
	}
	var step2 string
	for _, m := range p.reqs[1].Messages {
		step2 += m.Content + "\n"
	}
	if !strings.Contains(step2, "REPLY_LANDED") {
		t.Fatalf("step 2 missed the event:\n%s", step2)
	}
	if strings.Index(step2, "REPLY_LANDED") > strings.LastIndex(step2, "saw it") && strings.Contains(step2, "saw it") {
		t.Fatalf("event landed after the answer:\n%s", step2)
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	landed, answered := -1, -1
	for i, e := range entries {
		if strings.Contains(e.Message.Content, "REPLY_LANDED") {
			landed = i
		}
		if e.Message.Content == "saw it" {
			answered = i
		}
	}
	if landed < 0 || answered < 0 || landed > answered {
		t.Fatalf("order landed %d answered %d", landed, answered)
	}
}
