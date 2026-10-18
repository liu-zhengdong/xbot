package kernel

import (
	"context"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

// A line typed on another paired device reaches the model as the owner's own
// words, unwrapped, and ReplyAfter returns the answer once the turn is done.
func TestRemoteOwnerLineIsTrusted(t *testing.T) {
	b := testBot(t, link.NameRemoteOwner)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(b.SetOwners([]string{"u1"}))
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "answered"}}}
	k := &Kernel{Bot: b, Provider: p, Memory: memory.NopStore{}, MaxSteps: 4}
	ev := link.RemoteOwnerEvent("u1", "please check the build", time.Unix(10, 0).UTC())
	must(k.Handle(context.Background(), ev))
	if len(p.reqs) != 1 {
		t.Fatalf("provider calls %d", len(p.reqs))
	}
	var raw bool
	for _, m := range p.reqs[0].Messages {
		if m.Role == provider.RoleUser && m.Content == "please check the build" {
			raw = true
		}
	}
	if !raw {
		t.Fatal("the remote owner line was not the owner's own words")
	}
	text, done, err := ReplyAfter(b, ev.ID)
	if err != nil || !done || text != "answered" {
		t.Fatalf("text=%q done=%v err=%v", text, done, err)
	}
}

func TestReplyAfter(t *testing.T) {
	b := testBot(t, link.NameRemoteOwner)
	ev := link.RemoteOwnerEvent("u1", "how is the build?", time.Unix(10, 0).UTC())
	if err := b.Append(bot.Entry{Time: ev.Time, EventID: ev.ID, Message: provider.Message{Role: provider.RoleUser, Content: ev.Content}}); err != nil {
		t.Fatal(err)
	}
	// A turn that started but has only tool work so far is not done.
	if err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1"}}}}); err != nil {
		t.Fatal(err)
	}
	if _, done, err := ReplyAfter(b, ev.ID); err != nil || done {
		t.Fatalf("a turn with an open tool call is not done: done=%v err=%v", done, err)
	}
	if err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleTool, ToolCallID: "c1", Content: "ok"}}); err != nil {
		t.Fatal(err)
	}
	if _, done, err := ReplyAfter(b, ev.ID); err != nil || done {
		t.Fatalf("a turn without a final line is not done: done=%v err=%v", done, err)
	}
	if err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: "green"}}); err != nil {
		t.Fatal(err)
	}
	// A later turn belongs to someone else and does not change this answer.
	if err := b.Append(bot.Entry{EventID: "other", Message: provider.Message{Role: provider.RoleUser, Content: "hi"}}); err != nil {
		t.Fatal(err)
	}
	text, done, err := ReplyAfter(b, ev.ID)
	if err != nil || !done || text != "green" {
		t.Fatalf("text=%q done=%v err=%v", text, done, err)
	}
	if _, done, err := ReplyAfter(b, "never-seen"); err != nil || done {
		t.Fatalf("unknown event: done=%v err=%v", done, err)
	}
}
