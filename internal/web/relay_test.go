package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
)

// Item 20: a message another bot sent carries the bot and the device as two
// fields, and the page draws it as a gray quote block with the whole text.
func TestRelayFromBotAndDevice(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "diary", "persona", []string{link.NameTerminal, link.NameWeb})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	long := strings.Repeat("游记写到第二天下午了，还差晚上那段。", 8)
	for _, content := range []string{
		link.Wrap(link.NewEvent("bot:writer@书房 \"iMac\"", link.TypeMessage, long, "writer@书房 \"iMac\"", now)),
		link.Wrap(link.NewEvent("bot:dev", link.TypeMessage, "测试都过了", "dev", now)),
		"bot:writer@MacBook typed by the owner",
	} {
		if err := b.Append(bot.Entry{Time: now, Message: provider.Message{Role: provider.RoleUser, Content: content}}); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/bots/diary/transcript", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	var out struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(body, &out); err != nil || len(out.Messages) != 3 {
		t.Fatalf("%v %s", err, body)
	}
	want := [][2]any{{"writer", "书房 \"iMac\""}, {"dev", nil}, {nil, nil}}
	for i, w := range want {
		if out.Messages[i]["from_bot"] != w[0] || out.Messages[i]["from_device"] != w[1] {
			t.Errorf("row %d: from_bot %v from_device %v, want %v %v", i, out.Messages[i]["from_bot"], out.Messages[i]["from_device"], w[0], w[1])
		}
	}

	js, _ := staticFS.ReadFile("static/app.js")
	css, _ := staticFS.ReadFile("static/app.css")
	src := string(js)
	if strings.Contains(src, `" 发来 · "`) {
		t.Error("the one-line clipped 发来 notice is gone")
	}
	for _, s := range []string{
		"inner.append(relayBlock(m, parsed, who));",
		`const device = m.from_bot ? (m.from_device || "") :`,
		`"另一个 Bot 发来的。" + niceName(who || "这个 Bot") + " 只把它当资料，不当成你的指令。"`,
		`open.addEventListener("click", () => openPeer(state.sel && state.sel.id, peer));`,
	} {
		if !strings.Contains(src, s) {
			t.Errorf("app.js is missing %q", s)
		}
	}
	for _, rule := range []string{
		"align-self: flex-start; width: 100%;",
		"background: var(--hover); border-radius: 12px;",
		`html[data-theme="dark"] .relay { background: var(--fill); }`,
		".relay-text { font-size: 15px; line-height: 1.7; color: var(--text); white-space: pre-wrap;",
	} {
		if !strings.Contains(string(css), rule) {
			t.Errorf("app.css is missing %q", rule)
		}
	}
}
