package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
)

// Prototype v2 item 21: the exchange with a bot on a paired device comes from
// this bot's transcript alone: its message_bot calls, the replies, bounces.
func TestPeerMessagesRemote(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	diary := mustWebBot(t, root, "diary", "u1")
	t0 := time.Date(2026, 10, 18, 2, 39, 0, 0, time.UTC)
	call := func(id, to, text string, at time.Time) {
		t.Helper()
		args, _ := json.Marshal(map[string]string{"bot": to, "text": text})
		if err := diary.Append(bot.Entry{Time: at, Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
			{ID: id, Type: "function", Function: provider.FunctionCall{Name: "message_bot", Arguments: string(args)}},
		}}}); err != nil {
			t.Fatal(err)
		}
	}
	call("c1", "writer@MacBook", "写到哪了？", t0)
	call("c2", "writer@Other", "not this device", t0)
	appendWebDirect(t, diary, "ev_w1", "writer@MacBook", "第二天下午。", t0.Add(2*time.Minute))
	appendWebDirect(t, diary, "ev_local", "writer", "a local writer", t0.Add(2*time.Minute))
	call("c3", "writer@MacBook", "稍后再说。", t0.Add(3*time.Minute))
	bounce := link.NewEvent("remote", link.TypeMessage, "message to writer@MacBook was not delivered because the device is offline: 稍后再说。", "remote", t0.Add(4*time.Minute))
	appendWebUser(t, diary, "remote_undelivered_x", link.Wrap(bounce), t0.Add(4*time.Minute))

	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	get := func(path string) (int, []peerRow) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		var out struct {
			Messages []peerRow `json:"messages"`
		}
		_ = json.Unmarshal(raw, &out)
		return res.StatusCode, out.Messages
	}
	code, rows := get("/api/bots/diary/peers/writer@MacBook/messages")
	if code != 200 || len(rows) != 4 {
		t.Fatalf("%d %+v", code, rows)
	}
	want := []struct{ id, from, to, text, kind string }{
		{"c1", "diary", "writer@MacBook", "写到哪了？", ""},
		{"ev_w1", "writer@MacBook", "diary", "第二天下午。", ""},
		{"c3", "diary", "writer@MacBook", "稍后再说。", ""},
		{"remote_undelivered_x", "writer@MacBook", "diary", bounce.Content, "bounce"},
	}
	for i, w := range want {
		r := rows[i]
		if r.ID != w.id || r.From != w.from || r.To != w.to || r.Text != w.text || r.Kind != w.kind || r.TS.IsZero() {
			t.Errorf("row %d %+v, want %+v", i, r, w)
		}
	}
	code, rows = get("/api/bots/diary/peers/writer@MacBook/messages?limit=2&before=writer@MacBook:5")
	if code != 200 || len(rows) != 2 || rows[0].ID != "c1" || rows[1].ID != "ev_w1" {
		t.Fatalf("page back %d %+v", code, rows)
	}
	// Two calls in one reply share (To, Seq): a page never splits them, so
	// paging back from it loses neither.
	if err := diary.Append(bot.Entry{Time: t0.Add(5 * time.Minute), Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
		{ID: "c4", Type: "function", Function: provider.FunctionCall{Name: "message_bot", Arguments: `{"bot":"writer@MacBook","text":"one"}`}},
		{ID: "c5", Type: "function", Function: provider.FunctionCall{Name: "message_bot", Arguments: `{"bot":"writer@MacBook","text":"two"}`}},
	}}}); err != nil {
		t.Fatal(err)
	}
	code, rows = get("/api/bots/diary/peers/writer@MacBook/messages?limit=1")
	if code != 200 || len(rows) != 2 || rows[0].ID != "c4" || rows[1].ID != "c5" {
		t.Fatalf("batch page %d %+v", code, rows)
	}
	code, rows = get("/api/bots/diary/peers/writer@MacBook/messages?limit=1&before=writer@MacBook:7")
	if code != 200 || len(rows) != 1 || rows[0].ID != "remote_undelivered_x" {
		t.Fatalf("after batch %d %+v", code, rows)
	}
	for _, bad := range []string{"writer@", "@MacBook", "../x@MacBook"} {
		if code, _ := get("/api/bots/diary/peers/" + bad + "/messages"); code != http.StatusNotFound {
			t.Errorf("%s: %d", bad, code)
		}
	}
}
