package remote

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"xbot/internal/person"
)

// TestChatSummaries: the newest line is clip'd and stripped of marks, unread
// counts bot lines after the cursor (all of them before any read, none once
// the cursor points past them), queued is the oldest undelivered line, and a
// failed line raises the flag.
func TestChatSummaries(t *testing.T) {
	root := t.TempDir()
	dev := pairDevice(t, root)
	at := time.Now()
	write := func(lines ...ChatMessage) {
		t.Helper()
		for _, m := range lines {
			m.At = at
			mustRemote(t, appendChat(root, dev, "bob", m))
		}
	}
	write(
		ChatMessage{ID: "m1", Role: "me", Text: "  ## hi  ", State: "answered"},
		ChatMessage{ID: "b1", Role: "bot", Text: "**first** `answer`", State: "answered"},
		ChatMessage{ID: "m2", Role: "me", Text: "排队的一句", State: "queued_local"},
		ChatMessage{ID: "m3", Role: "me", Text: "later", State: "failed"},
		ChatMessage{ID: "b2", Role: "bot", Text: "- " + strings.Repeat("长", 80), State: "answered"},
	)
	sums, err := ChatSummaries(root, dev)
	mustRemote(t, err)
	s, ok := sums["bob"]
	if !ok || len(sums) != 1 {
		t.Fatalf("summaries: %+v", sums)
	}
	if s.Last != strings.Repeat("长", 72)+"…" || s.LastRole != "bot" {
		t.Fatalf("last: %q %q", s.Last, s.LastRole)
	}
	if s.Unread != 2 || !s.Failed || s.Queued != "排队的一句" {
		t.Fatalf("summary: %+v", s)
	}
	mustRemote(t, appendChat(root, dev, "bob", ChatMessage{ID: "b3", Role: "bot", Text: "[点这里](https://example.com) 就*好*了", At: at, State: "answered"}))
	sums, err = ChatSummaries(root, dev)
	mustRemote(t, err)
	if s := sums["bob"]; s.Last != "点这里 就好了" || s.Unread != 3 {
		t.Fatalf("markdown strip: %+v", s)
	}

	// A name the other computer could never have is skipped, not reported.
	mustRemote(t, appendChat(root, dev, "bad/name", ChatMessage{ID: "x1", Role: "bot", Text: "x"}))
	if sums, err = ChatSummaries(root, dev); err != nil || len(sums) != 1 {
		t.Fatalf("invalid name counted: %+v %v", sums, err)
	}

	// Reading the chat clears the unread count; a later bot line is one unread.
	mustRemote(t, MarkChatRead(root, dev, "bob", ""))
	if sums, _ = ChatSummaries(root, dev); sums["bob"].Unread != 0 {
		t.Fatalf("unread after read: %+v", sums["bob"])
	}
	mustRemote(t, appendChat(root, dev, "bob", ChatMessage{ID: "b4", Role: "bot", Text: "again", At: at, State: "answered"}))
	if sums, _ = ChatSummaries(root, dev); sums["bob"].Unread != 1 || sums["bob"].Last != "again" {
		t.Fatalf("unread after new line: %+v", sums["bob"])
	}

	// A cursor whose line the size cap trimmed counts the bot lines newer than its time.
	raw, err := json.Marshal(readCursor{ID: "trimmed", At: at})
	mustRemote(t, err)
	mustRemote(t, os.WriteFile(readPath(root, dev, "bob"), raw, 0600))
	if sums, _ = ChatSummaries(root, dev); sums["bob"].Unread != 0 || sums["bob"].Queued != "排队的一句" {
		t.Fatalf("trimmed cursor: %+v", sums["bob"])
	}
	mustRemote(t, appendChat(root, dev, "bob", ChatMessage{ID: "b5", Role: "bot", Text: "later", At: at.Add(time.Minute), State: "answered"}))
	if sums, _ = ChatSummaries(root, dev); sums["bob"].Unread != 1 {
		t.Fatalf("reply after a trimmed cursor: %+v", sums["bob"])
	}
	// Reading up to a given line leaves the later ones unread.
	mustRemote(t, MarkChatRead(root, dev, "bob", "b4"))
	if sums, _ = ChatSummaries(root, dev); sums["bob"].Unread != 1 {
		t.Fatalf("read up to b4: %+v", sums["bob"])
	}
	// A late view of an older snapshot does not move the cursor back.
	mustRemote(t, MarkChatRead(root, dev, "bob", ""))
	mustRemote(t, MarkChatRead(root, dev, "bob", "b4"))
	if sums, _ = ChatSummaries(root, dev); sums["bob"].Unread != 0 {
		t.Fatalf("cursor moved back: %+v", sums["bob"])
	}

	// No transcript at all is an empty map, not an error or a nil.
	if sums, err = ChatSummaries(root, strings.Repeat("cd", 16)); err != nil || sums == nil || len(sums) != 0 {
		t.Fatalf("unknown device: %+v %v", sums, err)
	}
}

// TestMarkChatReadErrors: an unknown device and an invalid name are refused,
// and a chat without lines leaves no cursor behind.
func TestMarkChatReadErrors(t *testing.T) {
	root := t.TempDir()
	dev := pairDevice(t, root)
	if err := MarkChatRead(root, strings.Repeat("ef", 16), "bob", ""); err == nil {
		t.Fatal("marked an unknown device read")
	}
	if err := MarkChatRead(root, dev, "../bob", ""); err == nil {
		t.Fatal("marked an invalid bot name read")
	}
	mustRemote(t, MarkChatRead(root, dev, "bob", ""))
	if _, err := os.Stat(readPath(root, dev, "bob")); err == nil {
		t.Fatal("a chat without lines got a cursor")
	}
}

// pairDevice adds one paired device "b" to root and returns its id.
func pairDevice(t *testing.T, root string) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(nil)
	mustRemote(t, err)
	_, xkey, err := XIdentity(t.TempDir())
	mustRemote(t, err)
	p, err := person.Identify(root, person.Account{Link: "terminal", ExternalID: "uid:1"}, "owner")
	mustRemote(t, err)
	d, err := AddDevice(root, "b", base64.StdEncoding.EncodeToString(pub), xkey, p.ID)
	mustRemote(t, err)
	return d.ID
}

// TestNoAck: only a sent owner line older than the timeout, while the device is
// online, is 「没收到回执」; offline it is queued, and acked lines never are.
func TestNoAck(t *testing.T) {
	now := time.Now()
	old := ChatMessage{Role: "me", State: "sent", At: now.Add(-ChatAckTimeout - time.Second)}
	if !NoAck(old, true, now) {
		t.Fatal("an old unacked line on an online device is not no_ack")
	}
	fresh := old
	fresh.At = now.Add(-time.Second)
	delivered := old
	delivered.State = "delivered"
	reply := old
	reply.Role = "bot"
	if NoAck(old, false, now) || NoAck(fresh, true, now) || NoAck(delivered, true, now) || NoAck(reply, true, now) {
		t.Fatal("no_ack for offline, fresh, delivered, or a bot line")
	}
}
