package peer

import (
	"os"
	"strings"
	"testing"
	"time"

	"xbot/internal/inbox"
)

func TestPeerMessageRuntimeLocalWriteFacts(t *testing.T) {
	root := t.TempDir()
	ada := mustPeerBot(t, root, "ada", "u1")
	bob := mustPeerBot(t, root, "bob", "u1")
	now := time.Date(2026, 11, 2, 5, 0, 0, 0, time.UTC)
	result, err := (&Bus{Root: root}).MessageWithResult("ada", "bob", "  body  ", "ch_original", 2, now)
	if err != nil || !result.Posted || result.EventID == "" || result.Stage != "complete" {
		t.Fatalf("actual acceptance %+v err=%v", result, err)
	}
	posts := readPosts(t, bob.InboxPath())
	if len(posts) != 1 || posts[0].ID != result.EventID || posts[0].Source != "bot:ada" || posts[0].Sender != "ada" || posts[0].SenderKind != inbox.KindBot || !posts[0].Untrusted || posts[0].Content != "body" || posts[0].Chain != "ch_original" || posts[0].Hops != 2 || !posts[0].Time.Equal(now) {
		t.Fatalf("original delivery %+v", posts)
	}
	if len(readPosts(t, ada.InboxPath())) != 0 {
		t.Fatal("message added a sender Inbox event")
	}
}

func TestPeerMessageRuntimeLocalLateErrorKeepsRate(t *testing.T) {
	root := t.TempDir()
	mustPeerBot(t, root, "ada", "u1")
	bob := mustPeerBot(t, root, "bob", "u1")
	if err := os.MkdirAll(bob.InboxPath(), 0700); err != nil {
		t.Fatal(err)
	}
	bus := &Bus{Root: root, Limit: 1, Window: time.Minute}
	now := time.Date(2026, 11, 2, 5, 0, 0, 0, time.UTC)
	result, err := bus.MessageWithResult("ada", "bob", "body", "ch_original", 1, now)
	if err == nil || result.Posted || result.EventID != "" || result.Stage != "recipient_open" {
		t.Fatalf("recipient fault acknowledgement %+v err=%v", result, err)
	}
	result, err = bus.MessageWithResult("ada", "bob", "body", "ch_original", 1, now)
	if err == nil || !strings.Contains(err.Error(), "rate") || result.Stage != "rate" || result.Posted || result.EventID != "" {
		t.Fatalf("consumed rate was hidden or refunded: %+v err=%v", result, err)
	}
}
