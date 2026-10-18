package remote

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

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
