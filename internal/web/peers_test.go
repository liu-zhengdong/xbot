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

func TestPeerMessages(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	ada := mustWebBot(t, root, "ada", "u1")
	bob := mustWebBot(t, root, "bob", "u1")
	mustWebBot(t, root, "cara", "u2")
	t0 := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)

	appendWebUser(t, ada, "ev_owner", "hello owner", t0.Add(-time.Minute))
	appendWebDirect(t, ada, "ev_early", "bob", "early", t0)
	appendWebDirect(t, ada, "ev_late", "bob", "<script>alert(1)</script>", t0.Add(3*time.Minute))
	appendWebUser(t, ada, "ev_group", link.Wrap(link.NewEvent("group:g_abcd1234", link.TypeMessage, "group hi", "bob", t0)), t0)
	appendWebDirect(t, ada, "ev_erin", "erin", "from erin", t0)

	appendWebDirect(t, bob, "ev_a1", "ada", "ada-1\nsecond", t0.Add(time.Minute))
	if err := bob.Append(bot.Entry{
		Time:    t0.Add(time.Minute),
		Message: provider.Message{Role: provider.RoleAssistant, Content: "noted"},
	}); err != nil {
		t.Fatal(err)
	}
	appendWebDirect(t, bob, "ev_a2", "ada", "ada-2", t0.Add(2*time.Minute))

	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	get := func(path string, auth bool) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if auth {
			req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
		}
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	body := func(res *http.Response) []byte {
		t.Helper()
		defer res.Body.Close()
		raw, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}

	res := get("/api/bots/ada/peers/bob/messages", false)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("auth %d", res.StatusCode)
	}
	res.Body.Close()

	for _, path := range []string{
		"/api/bots/ada/peers/ada/messages",
		"/api/bots/ada/peers/cara/messages",
		"/api/bots/ada/peers/missing/messages",
		"/api/bots/missing/peers/bob/messages",
	} {
		res = get(path, true)
		raw := body(res)
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("%s %d %s", path, res.StatusCode, raw)
		}
	}

	res = get("/api/bots/ada/peers/bob/messages?limit=0", true)
	raw := body(res)
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(string(raw), "bad limit") {
		t.Fatalf("limit %d %s", res.StatusCode, raw)
	}
	res = get("/api/bots/ada/peers/bob/messages?limit=201", true)
	raw = body(res)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("limit max %d %s", res.StatusCode, raw)
	}

	res = get("/api/bots/ada/peers/bob/messages", true)
	raw = body(res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list %d %s", res.StatusCode, raw)
	}
	if strings.Contains(string(raw), "<script>") || !strings.Contains(string(raw), `\u003cscript\u003e`) {
		t.Fatalf("escape %s", raw)
	}
	page := decodePeer(t, raw)
	want := []peerRow{
		{ID: "ev_early", Seq: 2, From: "bob", To: "ada", Text: "early", TS: t0},
		{ID: "ev_a1", Seq: 1, From: "ada", To: "bob", Text: "ada-1\nsecond", TS: t0.Add(time.Minute)},
		{ID: "ev_a2", Seq: 3, From: "ada", To: "bob", Text: "ada-2", TS: t0.Add(2 * time.Minute)},
		{ID: "ev_late", Seq: 3, From: "bob", To: "ada", Text: "<script>alert(1)</script>", TS: t0.Add(3 * time.Minute)},
	}
	if len(page) != len(want) {
		t.Fatalf("len %d %+v", len(page), page)
	}
	for i := range want {
		if page[i].ID != want[i].ID || page[i].Seq != want[i].Seq || page[i].From != want[i].From || page[i].To != want[i].To || page[i].Text != want[i].Text || !page[i].TS.Equal(want[i].TS) {
			t.Fatalf("row %d %+v", i, page[i])
		}
	}

	res = get("/api/bots/bob/peers/ada/messages?limit=2", true)
	raw = body(res)
	page = decodePeer(t, raw)
	if res.StatusCode != http.StatusOK || len(page) != 2 || page[0].ID != "ev_a2" || page[1].ID != "ev_late" {
		t.Fatalf("latest %d %+v %s", res.StatusCode, page, raw)
	}

	res = get("/api/bots/ada/peers/bob/messages?before=bob:3&limit=2", true)
	raw = body(res)
	page = decodePeer(t, raw)
	if res.StatusCode != http.StatusOK || len(page) != 2 || page[0].ID != "ev_early" || page[1].ID != "ev_a1" {
		t.Fatalf("older %d %+v %s", res.StatusCode, page, raw)
	}

	res = get("/api/bots/ada/peers/bob/messages?before=1&limit=10", true)
	raw = body(res)
	page = decodePeer(t, raw)
	if res.StatusCode != http.StatusOK || len(page) != 1 || page[0].ID != "ev_early" {
		t.Fatalf("bare %d %+v %s", res.StatusCode, page, raw)
	}

	res = get("/api/bots/ada/peers/bob/messages?before=3", true)
	raw = body(res)
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(string(raw), "bad before") {
		t.Fatalf("ambiguous %d %s", res.StatusCode, raw)
	}
	res = get("/api/bots/ada/peers/bob/messages?before=ada:99", true)
	raw = body(res)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing cursor %d %s", res.StatusCode, raw)
	}

	res = get("/api/bots/ada/peers/bob/messages?before=ada:3&limit=1", true)
	raw = body(res)
	page = decodePeer(t, raw)
	if len(page) != 1 || page[0].ID != "ev_a2" {
		t.Fatalf("one before late %+v", page)
	}

	emptyRoot := t.TempDir()
	empty := &Server{Root: emptyRoot, Owner: "u1"}
	emptyTok, _, err := empty.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	mustWebBot(t, emptyRoot, "ada", "u1")
	mustWebBot(t, emptyRoot, "bob", "u1")
	emptySrv := httptest.NewServer(muxOf(empty))
	defer emptySrv.Close()
	req, _ := http.NewRequest(http.MethodGet, emptySrv.URL+"/api/bots/ada/peers/bob/messages", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: emptyTok})
	res, err = emptySrv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw = body(res)
	page = decodePeer(t, raw)
	if res.StatusCode != http.StatusOK || len(page) != 0 || !strings.Contains(string(raw), `"messages":[]`) {
		t.Fatalf("empty %d %s", res.StatusCode, raw)
	}
}

func decodePeer(t *testing.T, raw []byte) []peerRow {
	t.Helper()
	var payload struct {
		Messages []peerRow `json:"messages"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Messages
}

func mustWebBot(t *testing.T, root, name, owner string) *bot.Bot {
	t.Helper()
	b, err := bot.Create(root, name, "persona", []string{link.NameTerminal, link.NameWeb})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{owner}); err != nil {
		t.Fatal(err)
	}
	return b
}

func appendWebDirect(t *testing.T, b *bot.Bot, id, from, text string, at time.Time) {
	t.Helper()
	ev := link.NewEvent("bot:"+from, link.TypeMessage, text, from, at)
	ev.ID = id
	appendWebUser(t, b, id, link.Wrap(ev), at)
}

func appendWebUser(t *testing.T, b *bot.Bot, id, content string, at time.Time) {
	t.Helper()
	if err := b.Append(bot.Entry{
		Time:    at,
		EventID: id,
		Message: provider.Message{Role: provider.RoleUser, Content: content},
	}); err != nil {
		t.Fatal(err)
	}
}
