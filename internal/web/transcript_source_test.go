package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
)

const macID = "0123456789abcdef0123456789abcdef"

// Owner lines carry from_device and wrapped bot events carry relay; an
// unreadable remote config falls back to raw values instead of failing.
func TestTranscriptSourceFields(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	user := func(id, content string) {
		t.Helper()
		if err := b.Append(bot.Entry{Time: now, EventID: id, Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(content)}}); err != nil {
			t.Fatal(err)
		}
	}
	wrapped := func(source, sender string) string {
		return link.Wrap(link.NewEvent(source, link.TypeMessage, "hi", sender, now))
	}
	user("rchat_"+macID+"_x", "typed on the mac")
	user("ev_local", "typed here")
	user("ev_bob", wrapped("bot:bob", "bob"))
	user("ev_writer", wrapped("bot:writer@MacBook", "writer@MacBook"))
	if err := b.Append(bot.Entry{Time: now, Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent(wrapped("bot:bob", "bob"))}}); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(root, "remote", "devices.json")
	save := func(raw string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cfgPath, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	save(`{"devices":[{"name":"MacBook","label":"我的 MacBook","id":"` + macID + `","person":"u1"}]}`)

	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	get := func() []apiRow {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/bots/ada/transcript", nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%d %s", res.StatusCode, raw)
		}
		var out struct {
			Messages []apiRow `json:"messages"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		if len(out.Messages) != 5 {
			t.Fatalf("rows %+v", out.Messages)
		}
		return out.Messages
	}
	rows := get()
	if d := rows[0].FromDevice; d == nil || d.ID != macID || d.Label != "我的 MacBook" {
		t.Fatalf("remote owner row: %+v", d)
	}
	if rows[1].FromDevice != nil || rows[1].Relay != nil {
		t.Fatalf("local owner row: %+v", rows[1])
	}
	if r := rows[2].Relay; r == nil || r.Bot != "bob" || r.Device != "" {
		t.Fatalf("local relay: %+v", r)
	}
	if r := rows[3].Relay; r == nil || r.Bot != "writer" || r.Device != "我的 MacBook" {
		t.Fatalf("device relay: %+v", r)
	}
	for _, row := range rows[2:] {
		if row.FromDevice != nil {
			t.Fatalf("seq %d carries from_device %+v", row.Seq, row.FromDevice)
		}
	}
	if rows[4].Relay != nil {
		t.Fatalf("assistant row carries relay %+v", rows[4].Relay)
	}

	save("{")
	rows = get()
	if d := rows[0].FromDevice; d == nil || d.ID != macID || d.Label != "" {
		t.Fatalf("unreadable config label: %+v", d)
	}
	if r := rows[3].Relay; r == nil || r.Bot != "writer" || r.Device != "MacBook" {
		t.Fatalf("unreadable config relay: %+v", r)
	}
	if r := rows[2].Relay; r == nil || r.Bot != "bob" || r.Device != "" {
		t.Fatalf("unreadable config local relay: %+v", r)
	}
}
