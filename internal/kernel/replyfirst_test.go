package kernel

import (
	"context"
	"strings"
	"testing"
	"time"

	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

func countReplyFirst(msgs []provider.Message) int {
	n := 0
	for _, m := range msgs {
		n += strings.Count(m.Text(), replyFirstNote)
	}
	return n
}

// TestReplyFirstReminderOncePerOwnerTurn checks the reply-first reminder rides
// the owner's message on the turn's first request and is not added again on
// the next tool step.
func TestReplyFirstReminderOncePerOwnerTurn(t *testing.T) {
	b := testBot(t, link.NameWeb)
	now := time.Unix(1000, 0).UTC()
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "send_message_to_user", Arguments: `{"text":"收到，我先看一下。"}`},
		}})},
		{Role: provider.RoleAssistant, Content: provider.TextContent("done")},
	}}
	k := &Kernel{
		Bot: b, Provider: p, Memory: memory.NopStore{}, MaxSteps: 4,
		Links: []link.Link{link.Terminal{}},
		Now:   func() time.Time { return now },
	}
	if err := k.Handle(context.Background(), link.WebEvent("u1", "hello", now.Add(time.Second))); err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) != 2 {
		t.Fatalf("requests %d, want 2", len(p.reqs))
	}
	first := p.reqs[0].Messages
	var owner provider.Message
	for _, m := range first {
		if m.Role == provider.RoleUser && strings.Contains(m.Text(), "\nhello\n</untrusted_event>") {
			owner = m
		}
	}
	if !strings.HasSuffix(owner.Text(), "\n\n"+replyFirstNote) {
		t.Fatalf("reminder is not appended to the owner line: %q", owner.Text())
	}
	if !strings.Contains(replyFirstNote, "send_message_to_user") || !strings.HasPrefix(replyFirstNote, "<system-reminder>") {
		t.Fatalf("reminder text %q", replyFirstNote)
	}
	if got := countReplyFirst(first); got != 1 {
		t.Fatalf("first request carries %d reminders, want 1", got)
	}
	second := p.reqs[1].Messages
	if got := countReplyFirst(second); got != 1 {
		t.Fatalf("second step carries %d reminders, want only the replayed one", got)
	}
	if strings.Contains(second[len(second)-1].Text(), replyFirstNote) {
		t.Fatal("second step appended the reminder again")
	}
	if strings.Contains(first[0].Text(), replyFirstNote) {
		t.Fatal("reminder was folded into the system prefix")
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Message.Text(), replyFirstNote) {
			t.Fatal("reminder was written into the transcript body")
		}
	}
}

// TestReplyFirstReminderOnlyOnOwnerChat checks timers, approvals and bot
// messages do not get the reminder.
func TestReplyFirstReminderOnlyOnOwnerChat(t *testing.T) {
	now := time.Unix(3000, 0).UTC()
	run := func(t *testing.T, ev link.Event) int {
		t.Helper()
		b := testBot(t, link.NameTerminal)
		p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("ok")}}}
		k := &Kernel{
			Bot: b, Provider: p, Memory: memory.NopStore{}, MaxSteps: 4,
			Links: []link.Link{link.Terminal{}, &link.Timer{}},
			Now:   func() time.Time { return now },
		}
		if err := k.Handle(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, r := range p.reqs {
			n += countReplyFirst(r.Messages)
		}
		return n
	}
	if run(t, link.TerminalEvent("u1", "hello", now.Add(time.Second))) != 1 {
		t.Fatal("terminal owner message did not carry the reminder")
	}
	if run(t, link.NewEvent(link.NameTimer, link.TypeTimer, "tick", "u1", now.Add(time.Second))) != 0 {
		t.Fatal("timer wake carried the reminder")
	}
	botEv := link.WebEvent("peer", "hi", now.Add(time.Second))
	botEv.SenderKind = link.KindBot
	if run(t, botEv) != 0 {
		t.Fatal("bot message carried the reminder")
	}
}
