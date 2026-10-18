package peer

import (
	"encoding/json"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
)

func TestRemoteConversationReadsBothHalves(t *testing.T) {
	root := t.TempDir()
	ada := mustPeerBot(t, root, "ada", "u1")
	t0 := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)

	appendDirect(t, ada, "ev_in", "writer@MacBook", "hello from the laptop", t0)
	appendRemoteCall(t, ada, "call_sent", "writer@MacBook", "ok then", "sent to writer@MacBook", t0.Add(time.Minute))
	appendRemoteCall(t, ada, "call_queued", "writer@MacBook", "later", "gateway unreachable; message to writer@MacBook queued and retried for up to 24 hours", t0.Add(2*time.Minute))
	appendRemoteCall(t, ada, "call_failed", "writer@MacBook", "nope", "remote: gateway rejected", t0.Add(3*time.Minute))
	appendRemoteCall(t, ada, "call_pending", "writer@MacBook", "waiting", "", t0.Add(4*time.Minute))
	appendBounce(t, ada, "remote_bounce_env1", "message to writer@MacBook was not delivered: no such bot", t0.Add(5*time.Minute))

	// Noise: another device, a local bot, and another bot on the same device.
	appendRemoteCall(t, ada, "call_other", "other@MacBook", "hi other", "sent to other@MacBook", t0.Add(6*time.Minute))
	appendDirect(t, ada, "ev_bob", "bob", "local hello", t0.Add(6*time.Minute))
	appendBounce(t, ada, "remote_undelivered_env2", "message to bob@MacBook was not delivered because run: hi", t0.Add(7*time.Minute))
	appendUser(t, ada, "ev_plain", "owner line", t0.Add(7*time.Minute))

	got, err := RemoteConversation(ada, "writer@MacBook")
	if err != nil {
		t.Fatal(err)
	}
	want := []Direct{
		{ID: "ev_in", Seq: 1, From: "writer@MacBook", To: "ada", Text: "hello from the laptop"},
		{ID: "call_sent", Seq: 2, From: "ada", To: "writer@MacBook", Text: "ok then", Status: statusSent},
		{ID: "call_queued", Seq: 4, From: "ada", To: "writer@MacBook", Text: "later", Status: statusQueued},
		{ID: "call_failed", Seq: 6, From: "ada", To: "writer@MacBook", Text: "nope", Status: statusFailed},
		{ID: "call_pending", Seq: 8, From: "ada", To: "writer@MacBook", Text: "waiting", Status: statusSent},
		{ID: "remote_bounce_env1", Seq: 9, From: "writer@MacBook", To: "ada", Text: "message to writer@MacBook was not delivered: no such bot", Kind: KindBounce},
	}
	if len(got) != len(want) {
		t.Fatalf("len %d %+v", len(got), got)
	}
	for i := range want {
		g := got[i]
		if g.ID != want[i].ID || g.Seq != want[i].Seq || g.From != want[i].From || g.To != want[i].To || g.Text != want[i].Text || g.Kind != want[i].Kind || g.Status != want[i].Status {
			t.Fatalf("row %d %+v want %+v", i, g, want[i])
		}
		if !g.Time.Equal(t0.Add(time.Duration(i) * time.Minute)) {
			t.Fatalf("time %d %v", i, g.Time)
		}
	}
	// trims the address and rejects addresses that cannot be a remote peer
	if trimmed, err := RemoteConversation(ada, " writer@MacBook "); err != nil || len(trimmed) != len(want) {
		t.Fatalf("trim %+v %v", trimmed, err)
	}
	for _, bad := range []string{"", "@MacBook", "writer@", "bad name@MacBook", "bob"} {
		if _, err := RemoteConversation(ada, bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	if got, err := RemoteConversation(ada, "nobody@MacBook"); err != nil || len(got) != 0 {
		t.Fatalf("other peer %+v %v", got, err)
	}
}

func appendRemoteCall(t *testing.T, b *bot.Bot, id, addr, text, result string, at time.Time) {
	t.Helper()
	args, err := json.Marshal(map[string]string{"bot": " " + addr + " ", "text": text})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Append(bot.Entry{
		Time: at,
		Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: id, Type: "function", Function: provider.FunctionCall{Name: "message_bot", Arguments: string(args)},
		}}},
	}); err != nil {
		t.Fatal(err)
	}
	if result == "" {
		return
	}
	if err := b.Append(bot.Entry{
		Time:    at,
		Message: provider.Message{Role: provider.RoleTool, ToolCallID: id, Content: result},
	}); err != nil {
		t.Fatal(err)
	}
}

func appendBounce(t *testing.T, b *bot.Bot, id, text string, at time.Time) {
	t.Helper()
	ev := link.NewEvent("remote", link.TypeMessage, text, "remote", at)
	ev.ID = id
	appendUser(t, b, id, link.Wrap(ev), at)
}

// One assistant entry may call message_bot to the same address twice; both are rows.
func TestRemoteConversationTwoCallsOneEntry(t *testing.T) {
	root := t.TempDir()
	a, err := bot.Create(root, "ada", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	call := func(id, text string) provider.ToolCall {
		return provider.ToolCall{ID: id, Type: "function", Function: provider.FunctionCall{Name: "message_bot", Arguments: `{"bot":"writer@MacBook","text":"` + text + `"}`}}
	}
	at := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	if err := a.Append(bot.Entry{Time: at, Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{call("c1", "one"), call("c2", "two")}}}); err != nil {
		t.Fatal(err)
	}
	rows, err := RemoteConversation(a, "writer@MacBook")
	if err != nil || len(rows) != 2 || rows[0].ID != "c1" || rows[1].ID != "c2" || rows[1].Text != "two" {
		t.Fatalf("%+v %v", rows, err)
	}
}
