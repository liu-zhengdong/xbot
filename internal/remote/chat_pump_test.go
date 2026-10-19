package remote

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
	"xbot/internal/gateway"
	"xbot/internal/link"
)

// TestPumpChatReplies delivers the bot's answer once its turn is done and drops
// the pending file; a 24 hour old pending file gives up.
func TestPumpChatReplies(t *testing.T) {
	a, b, ka, kb := chatPair(t)
	now := time.Now()
	_, aid, _, err := Identity(a)
	mustRemote(t, err)
	_, bid, _, err := Identity(b)
	mustRemote(t, err)
	m, err := SendOwnerChat(context.Background(), a, bid, "bob", "are we green?", now)
	mustRemote(t, err)
	clientB := &gateway.Client{Base: "http://gateway", Key: kb}
	receiveAll(t, b, bid, clientB, now)
	ev := bobInbox(t, b)[0]
	mustRemote(t, pumpChatReplies(context.Background(), b, clientB, now))
	if _, err := os.Stat(pendingPath(b, ev.ID)); err != nil {
		t.Fatal("pending file removed before the turn finished")
	}
	// B's bot finishes the turn (serve wires kernel.ReplyAfter; its own test covers that).
	TurnReply = func(_ *bot.Bot, e link.Event) (string, bool, error) { return "all green", e.ID == ev.ID, nil }
	t.Cleanup(func() { TurnReply = nil })
	mustRemote(t, pumpChatReplies(context.Background(), b, clientB, now))
	if _, err := os.Stat(pendingPath(b, ev.ID)); !os.IsNotExist(err) {
		t.Fatalf("pending file kept after the reply was sent: %v", err)
	}
	receiveAll(t, a, aid, &gateway.Client{Base: "http://gateway", Key: ka}, now)
	hist, err := ChatHistory(a, bid, "bob", 0)
	mustRemote(t, err)
	if len(hist) != 2 || hist[1].Text != "all green" || hist[0].State != "answered" || hist[1].Ref != m.ID {
		t.Fatalf("transcript after the reply: %+v", hist)
	}

	// A pending reply past 24 hours is deleted without sending.
	oldID := strings.Repeat("f", 16)
	old := chatPending{Device: aid, Bot: "bob", ID: oldID, At: now.Add(-25 * time.Hour)}
	old.Event = link.RemoteOwnerEvent("u1", "stale", now)
	old.Event.ID = "rchat_" + aid + "_" + oldID
	raw, err := json.Marshal(old)
	mustRemote(t, err)
	mustRemote(t, fsutil.WriteAtomic(pendingPath(b, old.Event.ID), raw, 0600))
	mustRemote(t, pumpChatReplies(context.Background(), b, clientB, now))
	if _, err := os.Stat(pendingPath(b, old.Event.ID)); !os.IsNotExist(err) {
		t.Fatal("stale pending reply kept")
	}
}

// A reply longer than MaxChatText used to be sent as-is: the asking computer
// drops it and still acks the envelope, so the answer vanished silently. The
// sender now shortens it and says so.
func TestClipChatTextKeepsLongReply(t *testing.T) {
	long := strings.Repeat("答", MaxChatText)
	got := clipChatText(long)
	if len(got) > MaxChatText {
		t.Fatalf("clipped length %d still above MaxChatText %d", len(got), MaxChatText)
	}
	if !strings.HasSuffix(got, chatCutNote) {
		t.Fatal("a shortened reply must say it was shortened")
	}
	if !utf8.ValidString(got) {
		t.Fatal("clipping split a rune")
	}
	if short := "简短回复"; clipChatText(short) != short {
		t.Fatal("a short reply was changed")
	}
}

// pendingChatReplies is what keeps the poll wait short while an answer is owed.
func TestPendingChatReplies(t *testing.T) {
	root := t.TempDir()
	if pendingChatReplies(root) {
		t.Fatal("empty root reports a pending reply")
	}
	dir := filepath.Join(root, "remote", "chatpending")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ev1.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if !pendingChatReplies(root) {
		t.Fatal("a pending reply file was not seen")
	}
}
