package kernel

import (
	"context"
	"encoding/json"
	"testing"
	"time"
	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/link"
)

func TestAskUserRequiresRecommendedOption(t *testing.T) {
	for _, args := range []string{
		`{"question":"下一步？","options":["A","B"]}`,
		`{"question":"下一步？","options":["A","B"],"recommended":"C"}`,
		`{"question":"下一步？","options":["A","B"],"recommended":1}`,
	} {
		if _, _, err := (askTool{}).Confirm(json.RawMessage(args)); err == nil {
			t.Fatalf("accepted %s", args)
		}
	}
	if _, _, err := (askTool{}).Confirm(json.RawMessage(`{"question":"下一步？","options":["A","B"],"recommended":"B"}`)); err != nil {
		t.Fatal(err)
	}
}

// A late choice belongs to its original card, even while two newer cards wait.
func TestQuestionChoicesDoNotAnswerOtherCards(t *testing.T) {
	b, err := bot.Create(t.TempDir(), "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, ApprovalWait: time.Hour}
	for _, text := range []string{"old", "first", "second"} {
		if err := k.askOwner(context.Background(), text, []string{"A", "B"}, "A"); err != nil {
			t.Fatal(err)
		}
	}
	var old, first, second *ownerQuestion
	questionsMu.Lock()
	for _, q := range questions {
		if botKey(q.b) != botKey(b) {
			continue
		}
		switch q.question {
		case "old":
			old = q
		case "first":
			first = q
		case "second":
			second = q
		}
	}
	if err := finishQuestionLocked(old, "A", true); err != nil {
		questionsMu.Unlock()
		t.Fatal(err)
	}
	questionsMu.Unlock()
	t.Cleanup(func() {
		questionsMu.Lock()
		defer questionsMu.Unlock()
		for key, q := range questions {
			if botKey(q.b) == botKey(b) {
				q.timer.Stop()
				delete(questions, key)
			}
		}
	})
	if err := ChooseQuestion(b, old.id, "B"); err != nil {
		t.Fatal(err)
	}
	questionsMu.Lock()
	done := first.done || second.done
	questionsMu.Unlock()
	if done {
		t.Fatal("late click answered a different pending card")
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	events := box.Pending()
	if len(events) != 1 || events[0].Source != link.NameApproval || events[0].Content != "B" {
		t.Fatalf("late choice must keep card source through Handle: %+v", events)
	}
	if err := ChooseQuestion(b, first.id, "A"); err != nil {
		t.Fatal(err)
	}
	questionsMu.Lock()
	defer questionsMu.Unlock()
	if !first.done || second.done {
		t.Fatal("choosing one card answered the other")
	}
}
