package kernel

import (
	"strings"
	"testing"
	"time"

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
// the owner produces one short note in the assembled request, so the model can
// remind the owner in passing.
func TestPendingApprovalReminderReachesTheModel(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	now := time.Unix(1000, 0).UTC()
	appendCard(t, b, "card-1", "shell", "mkdir approved-dir", bot.ApprovalPending, now.Add(-90*time.Second))
	k := &Kernel{
		Bot: b, Memory: memory.NopStore{}, MaxSteps: 4,
		Links: []link.Link{link.Terminal{}},
		Now:   func() time.Time { return now },
	}
	got := k.pendingHistory()
	if len(got) != 1 {
		t.Fatalf("pending note messages %d, want 1", len(got))
	}
	text := got[0].Content
	for _, want := range []string{"<pending_approvals>", "shell", "mkdir approved-dir", "1m", "</pending_approvals>"} {
		if !strings.Contains(text, want) {
			t.Fatalf("note %q missing %q", text, want)
		}
	}
	if got[0].Role != provider.RoleUser {
		t.Fatalf("note role %q, want user", got[0].Role)
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
	if got := k.pendingHistory(); len(got) != 0 {
		t.Fatalf("decided cards still produced a note: %v", got)
	}
	appendCard(t, b, "card-3", "http_request", "GET example.test", bot.ApprovalPending, now.Add(-30*time.Second))
	got := k.pendingHistory()
	if len(got) != 1 || !strings.Contains(got[0].Content, "http_request") {
		t.Fatalf("open card not reported: %v", got)
	}
	if strings.Contains(got[0].Content, "card-1") || strings.Contains(got[0].Content, "mkdir a") {
		t.Fatalf("answered card leaked into the note: %v", got[0].Content)
	}
}

// TestPendingApprovalNoteOmitsWhenNothingIsOpen checks the everyday case: no open
// card means no extra message at all.
func TestPendingApprovalNoteOmitsWhenNothingIsOpen(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, Memory: memory.NopStore{}, Links: []link.Link{link.Terminal{}}}
	if got := k.pendingHistory(); len(got) != 0 {
		t.Fatalf("empty transcript produced a note: %v", got)
	}
	if note := pendingNote(nil); note != "" {
		t.Fatalf("empty list rendered %q", note)
	}
}

// TestPendingApprovalNoteIsNotInTheStablePrefix guards the prompt cache: the note
// must ride with the history, not sit in the system prefix.
func TestPendingApprovalNoteIsNotInTheStablePrefix(t *testing.T) {
	list := []PendingApproval{{Tool: "shell", Summary: "运行命令 mkdir a", Age: 2 * time.Minute}}
	msgs, _ := Assemble(Input{Persona: "p", Rules: "r", Pending: list})
	if len(msgs) == 0 {
		t.Fatal("no messages")
	}
	if strings.Contains(msgs[0].Content, "<pending_approvals>") {
		t.Fatal("pending note was folded into the system prefix")
	}
	last := msgs[len(msgs)-1]
	if !strings.Contains(last.Content, "<pending_approvals>") || last.Role != provider.RoleUser {
		t.Fatalf("note not appended after the history: %+v", last)
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
	if got := k.pendingHistory(); len(got) != 0 {
		t.Fatalf("declined card still noted: %v", got)
	}
}
