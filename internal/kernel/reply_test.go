package kernel

import (
	"context"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

func TestQuoteRoundTripAndTruncation(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("字", 300) + "TAILMARKER"
	if err := b.Append(bot.Entry{
		Time:    time.Unix(1, 0).UTC(),
		Message: provider.Message{Role: provider.RoleAssistant, Content: long},
	}); err != nil {
		t.Fatal(err)
	}
	var seen []string
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: "reply_to: 2\nnoted"},
	}}
	k := &Kernel{Bot: b, Provider: p, Memory: memory.NopStore{}, MaxSteps: 4, OnDelta: func(s string) {
		seen = append(seen, s)
	}}
	ev := link.TerminalEvent("u1", "and this", time.Unix(2, 0).UTC())
	ev.ReplyTo = 1
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if k.ReplySeq() != 2 {
		t.Fatalf("reply seq %d", k.ReplySeq())
	}
	joined := strings.Join(seen, "")
	if strings.Contains(joined, "reply_to:") || joined != "noted" {
		t.Fatalf("stream %q", joined)
	}
	body := messagesText(p.reqs[0].Messages)
	quote := ""
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "user is replying to your earlier message (seq 1): ") {
			quote = line
			break
		}
	}
	if quote == "" {
		t.Fatalf("quote missing\n%s", body)
	}
	if strings.Contains(quote, "TAILMARKER") || !strings.HasSuffix(quote, "…") {
		t.Fatalf("quote %q", quote)
	}
	full, err := k.Expand(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(full, "TAILMARKER") {
		t.Fatalf("expand lost the tail\n%s", full)
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var user, asst *bot.Entry
	for i := range entries {
		e := &entries[i]
		if e.EventID == ev.ID {
			user = e
		}
		if e.Message.Role == provider.RoleAssistant && e.Message.Content == "noted" {
			asst = e
		}
	}
	if user == nil || user.ReplyTo != 1 || user.Message.Content != "and this" {
		t.Fatalf("user entry %+v", user)
	}
	if asst == nil || asst.ReplyTo != 2 || strings.Contains(asst.Message.Content, "reply_to:") {
		t.Fatalf("assistant %+v", asst)
	}
}

func TestReplyToIgnoresNonUserSeq(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: "reply_to: 2\nstill here"},
	}}
	k := &Kernel{Bot: b, Provider: p, Memory: memory.NopStore{}, MaxSteps: 2}
	ev := link.TerminalEvent("u1", "hello", time.Unix(2, 0).UTC())
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if k.ReplySeq() != 0 {
		t.Fatalf("seq %d", k.ReplySeq())
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	last := entries[len(entries)-1]
	if last.ReplyTo != 0 || last.Message.Content != "still here" {
		t.Fatalf("stored %+v", last)
	}
}
