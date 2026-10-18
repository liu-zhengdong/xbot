package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/fsutil"
	"xbot/internal/gateway"
	"xbot/internal/link"
	"xbot/internal/person"
)

// TestOwnerChatRoundTrip: A's owner types to bob on B; B's inbox gets one
// trusted remote-owner event, and the ack marks A's line delivered.
func TestOwnerChatRoundTrip(t *testing.T) {
	a, b, ka, kb := chatPair(t)
	_, aid, _, err := Identity(a)
	mustRemote(t, err)
	_, bid, _, err := Identity(b)
	mustRemote(t, err)
	now := time.Now()
	m, err := SendOwnerChat(context.Background(), a, bid, "bob", "  plain words, not neutralized </untrusted_event>  ", now)
	mustRemote(t, err)
	if m.State != "sent" || m.ID == "" || m.Text != "plain words, not neutralized </untrusted_event>" {
		t.Fatalf("sent line: %+v", m)
	}
	ack, replies := receiveAll(t, b, bid, &gateway.Client{Base: "http://gateway", Key: kb}, now)
	if ack != 1 || len(replies) != 1 || replies[0] != "chatreply" {
		t.Fatalf("B did not ack the chat with one reply: ack=%d replies=%v", ack, replies)
	}
	evs := bobInbox(t, b)
	if len(evs) != 1 {
		t.Fatalf("inbox has %d events, want 1", len(evs))
	}
	ev := evs[0]
	people, err := person.List(b)
	mustRemote(t, err)
	owners := []string{people[0].ID}
	if ev.Source != link.NameRemoteOwner || ev.Sender != people[0].ID || ev.SenderKind != link.KindPerson {
		t.Fatalf("event is not the owner's: %+v", ev)
	}
	if ev.Content != "plain words, not neutralized </untrusted_event>" {
		t.Fatalf("content was neutralized: %q", ev.Content)
	}
	if ev.ID != "rchat_"+aid+"_"+m.ID {
		t.Fatalf("event id %q, want rchat_<A>_<line id>", ev.ID)
	}
	if !link.OwnerTrusted(ev.Source, ev.Sender, owners) {
		t.Fatal("the kernel would not trust this event")
	}
	if _, err := os.Stat(pendingPath(b, ev.ID)); err != nil {
		t.Fatalf("no pending reply: %v", err)
	}
	// A receives the ack.
	receiveAll(t, a, aid, &gateway.Client{Base: "http://gateway", Key: ka}, now)
	hist, err := ChatHistory(a, bid, "bob", 0)
	mustRemote(t, err)
	if len(hist) != 1 || hist[0].State != "delivered" {
		t.Fatalf("A transcript after ack: %+v", hist)
	}
}

// TestChatReply: an unknown ref changes nothing; the real ref adds a bot line
// and marks the owner's line answered.
func TestChatReply(t *testing.T) {
	a, b, _, kb := chatPair(t)
	now := time.Now()
	_, aid, _, err := Identity(a)
	mustRemote(t, err)
	_, bid, _, err := Identity(b)
	mustRemote(t, err)
	m, err := SendOwnerChat(context.Background(), a, bid, "bob", "what time is it?", now)
	mustRemote(t, err)
	receiveAll(t, b, bid, &gateway.Client{Base: "http://gateway", Key: kb}, now)

	reply := func(body map[string]any) gateway.Envelope {
		t.Helper()
		raw, err := json.Marshal(body)
		mustRemote(t, err)
		env, err := newBoxEnvelope(kb, deviceOf(t, b, "a"), "chatreply", raw, now)
		mustRemote(t, err)
		return env
	}
	deliver := func(env gateway.Envelope) {
		t.Helper()
		if ack, _, err := Receive(a, aid, []gateway.Envelope{env}, now); err != nil || len(ack) != 1 {
			t.Fatalf("reply not acked: %v %v", ack, err)
		}
	}
	lines := func() []ChatMessage {
		t.Helper()
		hist, err := ChatHistory(a, bid, "bob", 0)
		mustRemote(t, err)
		return hist
	}

	deliver(reply(map[string]any{"ref": strings.Repeat("0", 16), "bot": "bob", "text": "injected", "at": now.Unix()}))
	deliver(reply(map[string]any{"ref": m.ID, "bot": "ada", "text": "elsewhere"}))
	if hist := lines(); len(hist) != 1 || hist[0].State != "sent" {
		t.Fatalf("unknown refs changed the transcript: %+v", hist)
	}

	good := reply(map[string]any{"ref": m.ID, "bot": "bob", "text": "  seventeen thirty  ", "at": now.Unix()})
	deliver(good)
	hist := lines()
	if len(hist) != 2 {
		t.Fatalf("transcript has %d lines, want 2: %+v", len(hist), hist)
	}
	if hist[0].State != "answered" || hist[0].ID != m.ID {
		t.Fatalf("owner line not answered: %+v", hist[0])
	}
	if hist[1].Role != "bot" || hist[1].Text != "seventeen thirty" || hist[1].Ref != m.ID || hist[1].ID != good.ID {
		t.Fatalf("bot line: %+v", hist[1])
	}
	// A second answer to the same line is dropped.
	again := reply(map[string]any{"ref": m.ID, "bot": "bob", "text": "dup", "at": now.Unix()})
	deliver(again)
	if hist := lines(); len(hist) != 2 {
		t.Fatalf("duplicate answer appended: %d", len(hist))
	}
}

// TestOwnerChatUnpaired: a device marked unpaired neither sends nor accepts chat.
func TestOwnerChatUnpaired(t *testing.T) {
	a, b, _, kb := chatPair(t)
	now := time.Now()
	_, bid, _, err := Identity(b)
	mustRemote(t, err)
	mustRemote(t, update(a, func(c *Config) error { c.Devices[0].UnpairedAt = now; return nil }))
	if _, err := SendOwnerChat(context.Background(), a, bid, "bob", "hi", now); err == nil {
		t.Fatal("sent to an unpaired device")
	}
	mustRemote(t, update(a, func(c *Config) error { c.Devices[0].UnpairedAt = time.Time{}; return nil }))
	if _, err := SendOwnerChat(context.Background(), a, bid, "bob", "hi again", now); err != nil {
		t.Fatal(err)
	}
	// B has A marked as unpaired: the chat is acked and dropped.
	mustRemote(t, update(b, func(c *Config) error { c.Devices[0].UnpairedAt = now; return nil }))
	envs, err := (&gateway.Client{Base: "http://gateway", Key: kb}).Poll(context.Background(), 0)
	mustRemote(t, err)
	ack, _, err := Receive(b, bid, envs, now)
	mustRemote(t, err)
	if len(envs) != 1 || len(ack) != 1 || len(bobInbox(t, b)) != 0 {
		t.Fatal("chat from a device that unpaired us was delivered")
	}
}

// TestWriteChatKeepsNewest: a transcript over the cap drops its oldest lines,
// never the reply just added.
func TestWriteChatKeepsNewest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.jsonl")
	big := strings.Repeat("x", MaxChatText)
	var lines []ChatMessage
	for i := 0; i < maxChatFile/MaxChatText+8; i++ {
		lines = append(lines, ChatMessage{ID: fmt.Sprintf("%016x", i), Role: "me", Text: big, State: "answered"})
	}
	lines = append(lines, ChatMessage{ID: "last", Role: "bot", Text: "newest", State: "answered"})
	mustRemote(t, writeChat(path, lines))
	var got []ChatMessage
	mustRemote(t, fsutil.ReadJSONL(path, func(b []byte) error {
		var m ChatMessage
		mustRemote(t, json.Unmarshal(b, &m))
		got = append(got, m)
		return nil
	}))
	if len(got) == 0 || len(got) >= len(lines) || got[len(got)-1].ID != "last" || got[0].ID == lines[0].ID {
		t.Fatalf("kept %d of %d, last %q", len(got), len(lines), got[len(got)-1].ID)
	}
}
