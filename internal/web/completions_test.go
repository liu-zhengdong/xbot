package web

import (
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
)

func TestCompletedUnreadTracksOwnerCursor(t *testing.T) {
	s, _, bots := statusFixture(t, 1)
	b := bots[0]
	appendResult := func(kind, origin string) {
		t.Helper()
		err := b.Append(bot.Entry{Time: time.Now(), Origin: origin, Message: provider.Message{Role: provider.RoleUser, Content: link.Render(link.Event{Source: link.SourceSubagent, Sender: "sa_done", Type: kind, Content: "完成", Untrusted: true}, false)}})
		if err != nil {
			t.Fatal(err)
		}
	}
	appendResult("done", bot.OriginOwner)
	if n := s.unreadCompletions(b, 0); n != 1 {
		t.Fatalf("visible owner-origin completion count: %d, want 1", n)
	}
	appendResult("progress", bot.OriginOwner)
	appendResult("done", bot.OriginInternal)
	if n := s.unreadCompletions(b, 0); n != 1 {
		t.Fatalf("progress or hidden internal result was counted: %d", n)
	}
	appendResult("done", "") // Visible history written before origin was recorded.
	for i := 0; i < 2; i++ {
		if n := s.unreadCompletions(b, 0); n != 2 {
			t.Fatalf("poll %d: %d completions", i, n)
		}
	}
	before, _ := b.Count()
	if err := s.writeCursor(b.Name(), before); err != nil {
		t.Fatal(err)
	}
	if n := s.botStatuses()[0].CompletedUnread; n != 0 {
		t.Fatalf("viewed result still unread: %d", n)
	}
	appendResult("failed", bot.OriginOwner)
	if n := s.botStatuses()[0].CompletedUnread; n != 1 {
		t.Fatalf("new result count: %d", n)
	}
	appendResult("fork_end", bot.OriginOwner)
	if n := s.botStatuses()[0].CompletedUnread; n != 2 {
		t.Fatalf("fork result not counted: %d", n)
	}

}
