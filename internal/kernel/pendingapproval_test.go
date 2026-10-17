package kernel

import (
	"context"
	"strings"
	"testing"
	"time"

	"xbot/internal/agent"
	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

// appendCard writes one approval card into the transcript.
func appendCard(t *testing.T, b *bot.Bot, id, tool, summary, state string, at time.Time) {
	t.Helper()
	content := bot.ApprovalCardText(id, tool, "运行命令", "在本机上执行", summary, state)
	if err := b.Append(bot.Entry{Time: at, Message: provider.Message{Role: provider.RoleUser, Content: content}}); err != nil {
		t.Fatal(err)
	}
}

// TestPendingApprovalReminderReachesTheModel checks that a card still waiting for
// the owner is appended to the newest message, not sent as its own message, so
// the model can mention it without a second user turn.
func TestPendingApprovalReminderReachesTheModel(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1000, 0).UTC()
	appendCard(t, b, "card-1", "shell", "mkdir approved-dir", bot.ApprovalPending, now.Add(-90*time.Second))
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "ok"}}}
	k := &Kernel{
		Bot: b, Provider: p, Memory: memory.NopStore{}, MaxSteps: 4,
		Links: []link.Link{link.Terminal{}},
		Now:   func() time.Time { return now },
	}
	ev := link.TerminalEvent("u1", "hello", now.Add(time.Second))
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) != 1 {
		t.Fatalf("requests %d", len(p.reqs))
	}
	msgs := p.reqs[0].Messages
	var hits int
	var carrier provider.Message
	for _, m := range msgs {
		if strings.Contains(m.Content, "<pending_approvals>") {
			hits++
			carrier = m
		}
	}
	if hits != 1 {
		t.Fatalf("reminder messages %d, want 1 on the newest message", hits)
	}
	if !strings.Contains(carrier.Content, "hello") {
		t.Fatalf("reminder was not appended to the owner message: %q", carrier.Content)
	}
	if strings.HasPrefix(strings.TrimSpace(carrier.Content), "<pending_approvals>") {
		t.Fatal("reminder replaced the newest message instead of appending")
	}
	for _, want := range []string{"shell", "mkdir approved-dir", "1m", "</pending_approvals>"} {
		if !strings.Contains(carrier.Content, want) {
			t.Fatalf("note %q missing %q", carrier.Content, want)
		}
	}
	if strings.Contains(msgs[0].Content, "<pending_approvals>") {
		t.Fatal("pending note was folded into the system prefix")
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Message.Role == provider.RoleUser && strings.Contains(e.Message.Content, "hello") && strings.Contains(e.Message.Content, "<pending_approvals>") {
			t.Fatal("reminder was written into the transcript body")
		}
	}
}

// TestPendingApprovalReminderStopsAfterTheAnswer checks a decided card no longer
// produces a reminder: only cards still open count.
func TestPendingApprovalReminderStopsAfterTheAnswer(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	now := time.Unix(1000, 0).UTC()
	appendCard(t, b, "card-1", "shell", "mkdir a", bot.ApprovalPending, now.Add(-time.Minute))
	appendCard(t, b, "card-1", "shell", "mkdir a", bot.ApprovalApproved, now)
	appendCard(t, b, "card-2", "shell", "mkdir b", bot.NoDecisionText(bot.ApprovalReasonExpired), now)
	k := &Kernel{
		Bot: b, Memory: memory.NopStore{}, MaxSteps: 4,
		Links: []link.Link{link.Terminal{}},
		Now:   func() time.Time { return now },
	}
	if due, err := k.reminderKinds(link.Event{Source: link.NameTerminal}).Due(now); err != nil || len(due) != 0 {
		t.Fatalf("decided cards still produced a note: %v %v", due, err)
	}
	appendCard(t, b, "card-3", "http_request", "GET example.test", bot.ApprovalPending, now.Add(-30*time.Second))
	due, err := k.reminderKinds(link.Event{Source: link.NameTerminal}).Due(now)
	if err != nil || len(due) != 1 || !strings.Contains(due[0].Text, "http_request") {
		t.Fatalf("open card not reported: %v %v", due, err)
	}
	if strings.Contains(due[0].Text, "mkdir a") {
		t.Fatalf("answered card leaked into the note: %v", due[0].Text)
	}
}

// TestPendingApprovalNoteOmitsWhenNothingIsOpen checks the everyday case: no open
// card means no extra message at all.
func TestPendingApprovalNoteOmitsWhenNothingIsOpen(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, Memory: memory.NopStore{}, Links: []link.Link{link.Terminal{}}}
	due, err := k.reminderKinds(link.Event{Source: link.NameTerminal}).Due(time.Unix(1, 0).UTC())
	if err != nil || len(due) != 0 {
		t.Fatalf("empty transcript produced a note: %v %v", due, err)
	}
	if note := pendingNote(nil); note != "" {
		t.Fatalf("empty list rendered %q", note)
	}
}

// TestPendingApprovalNoteIsNotInTheStablePrefix guards the prompt cache: the note
// must ride with the history, not sit in the system prefix.
func TestPendingApprovalNoteIsNotInTheStablePrefix(t *testing.T) {
	list := []PendingApproval{{Tool: "shell", Summary: "运行命令 mkdir a", Age: 2 * time.Minute}}
	hist := []bot.Entry{{Message: provider.Message{Role: provider.RoleUser, Content: "hello"}}}
	msgs, _ := Assemble(Input{Persona: "p", Rules: "r", Entries: hist})
	if len(msgs) == 0 {
		t.Fatal("no messages")
	}
	prefix := msgs[0].Content
	before := append([]provider.Message(nil), msgs...)
	msgs = agent.AttachReminder(msgs, pendingNote(list))
	if msgs[0].Content != prefix || strings.Contains(msgs[0].Content, "<pending_approvals>") {
		t.Fatal("pending note was folded into the system prefix")
	}
	if before[len(before)-1].Content == msgs[len(msgs)-1].Content {
		t.Fatal("note was not appended to the newest message")
	}
	last := msgs[len(msgs)-1]
	if !strings.HasPrefix(last.Content, "hello") || !strings.Contains(last.Content, "<pending_approvals>") || last.Role != provider.RoleUser {
		t.Fatalf("note not appended to the newest message: %+v", last)
	}
	if len(msgs) != len(before) {
		t.Fatalf("attach added a message: %d -> %d", len(before), len(msgs))
	}
}

// TestPendingApprovalNoteTextIsShort keeps the note from becoming a second
// conversation: one line per card, no transcript replay.
func TestPendingApprovalNoteTextIsShort(t *testing.T) {
	list := []PendingApproval{
		{Tool: "shell", Summary: strings.Repeat("long summary ", 40), Age: 3 * time.Hour},
	}
	note := pendingNote(list)
	if strings.Count(note, "\n") > 4 {
		t.Fatalf("note too long:\n%s", note)
	}
	if !strings.HasSuffix(note, "</pending_approvals>") {
		t.Fatalf("note not closed: %q", note)
	}
	if !strings.Contains(note, "3h") {
		t.Fatalf("age missing: %q", note)
	}
}

// TestPendingApprovalReminderOnlyOnOwnerMessages checks the reminder is tied to
// a message from the owner. A scheduled wake must not carry it: the timer rules
// let such a wake end in SILENT, and an unsolicited reminder would defeat that.
func TestPendingApprovalReminderOnlyOnOwnerMessages(t *testing.T) {
	now := time.Unix(3000, 0).UTC()
	hasNote := func(reqs []provider.Request) bool {
		for _, r := range reqs {
			for _, m := range r.Messages {
				if strings.Contains(m.Content, "<pending_approvals>") {
					return true
				}
			}
		}
		return false
	}
	run := func(t *testing.T, ev link.Event) bool {
		t.Helper()
		b := testBot(t, link.NameTerminal)
		appendCard(t, b, "card-1", "shell", "mkdir a", bot.ApprovalPending, now.Add(-time.Minute))
		p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "ok"}}}
		k := &Kernel{
			Bot: b, Provider: p, Memory: memory.NopStore{}, MaxSteps: 4,
			Links: []link.Link{link.Terminal{}, &link.Timer{}},
			Now:   func() time.Time { return now },
		}
		if err := k.Handle(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
		return hasNote(p.reqs)
	}
	if !run(t, link.TerminalEvent("u1", "hello", time.Unix(3001, 0).UTC())) {
		t.Fatal("owner message did not carry the pending-approval reminder")
	}
	timerEv := link.NewEvent(link.NameTimer, link.TypeTimer, "tick", "u1", time.Unix(3002, 0).UTC())
	if run(t, timerEv) {
		t.Fatal("timer wake carried the pending-approval reminder")
	}
}

// TestOpenApprovalsReadsLatestCardState pins the transcript as the single source
// of truth: the newest card for an id decides.
func TestOpenApprovalsReadsLatestCardState(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	now := time.Unix(2000, 0).UTC()
	appendCard(t, b, "c1", "shell", "mkdir a", bot.ApprovalPending, now.Add(-2*time.Minute))
	appendCard(t, b, "c1", "shell", "mkdir a", bot.ApprovalDeclined, now.Add(-time.Minute))
	k := &Kernel{Bot: b, Memory: memory.NopStore{}, Now: func() time.Time { return now }}
	if open := k.openApprovals(); len(open) != 0 {
		t.Fatalf("declined card still open: %v", open)
	}
	if _, _, err := k.assemble(bot.State{}, ""); err != nil {
		t.Fatal(err)
	}
	due, err := k.reminderKinds(link.Event{Source: link.NameTerminal}).Due(now)
	if err != nil || len(due) != 0 {
		t.Fatalf("declined card still noted: %v %v", due, err)
	}
}
