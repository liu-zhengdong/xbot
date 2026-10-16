package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/fsutil"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/mockopenai"
	"xbot/internal/peer"
	"xbot/internal/proc"
	"xbot/internal/provider"
	"xbot/internal/web"
)

func TestWebAuthAndOrigin(t *testing.T) {
	root := t.TempDir()
	ui := &web.Server{Root: root, Owner: "u1"}
	tok, _, err := ui.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(root, "web-token.json"))
	if err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("token mode %v %v", fi, err)
	}
	srv := httptest.NewServer(webMux(ui))
	defer srv.Close()

	res := doJSON(t, srv.Client(), http.MethodPost, srv.URL+"/api/login", `{"token":"not-the-token"}`, "")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token %d", res.StatusCode)
	}
	res.Body.Close()

	res = doJSON(t, srv.Client(), http.MethodGet, srv.URL+"/api/bots", "", "")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no cookie %d", res.StatusCode)
	}
	res.Body.Close()

	client := loginWeb(t, srv, tok)
	res = doJSON(t, client, http.MethodGet, srv.URL+"/api/session", "", "")
	body := readBody(t, res)
	if res.StatusCode != 200 || !strings.Contains(body, `"owner":"u1"`) {
		t.Fatalf("session %d %s", res.StatusCode, body)
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/bots", strings.NewReader(`{"name":"ada","persona":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://evil.example")
	res, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("origin %d", res.StatusCode)
	}
	res.Body.Close()

	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/api/logout", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	res, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("sec-fetch %d", res.StatusCode)
	}
	res.Body.Close()
}

func TestWebAndTerminalShareTranscript(t *testing.T) {
	root, client, ui, _ := m5Stack(t)
	ada := mustM5Bot(t, root, "ada", "You are Ada.")
	srv := httptest.NewServer(webMux(ui))
	defer srv.Close()
	webc := loginWeb(t, srv, mustToken(t, ui))

	res := doJSON(t, webc, http.MethodPost, srv.URL+"/api/bots/ada/messages", `{"text":"HELLO_WEB"}`, "")
	body := readBody(t, res)
	if res.StatusCode != 200 || !strings.Contains(body, "web hello") {
		t.Fatalf("stream %d %s", res.StatusCode, body)
	}
	transcript := getBody(t, webc, srv.URL+"/api/bots/ada/transcript")
	if !strings.Contains(transcript, "HELLO_WEB") || strings.Contains(transcript, "HELLO_WEB") && strings.Contains(between(transcript, "HELLO_WEB"), "<untrusted_event") {
		t.Fatalf("web text wrapped or missing:\n%s", transcript)
	}
	if strings.Contains(rawUser(t, ada, "HELLO_WEB"), "<untrusted_event") {
		t.Fatal("owner web message was wrapped")
	}
	if !strings.Contains(transcript, "web hello") {
		t.Fatalf("missing reply:\n%s", transcript)
	}

	k, err := m5Kernel(ada, client)
	if err != nil {
		t.Fatal(err)
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "FROM_TERMINAL", time.Now())); err != nil {
		t.Fatal(err)
	}
	transcript = getBody(t, webc, srv.URL+"/api/bots/ada/transcript")
	if !strings.Contains(transcript, "HELLO_WEB") || !strings.Contains(transcript, "FROM_TERMINAL") || !strings.Contains(transcript, "terminal hello") {
		t.Fatalf("shared transcript:\n%s", transcript)
	}
	if strings.Contains(rawUser(t, ada, "FROM_TERMINAL"), "<untrusted_event") {
		t.Fatal("owner terminal message was wrapped")
	}
}

func TestWebQueuesWhileTerminalHoldsLock(t *testing.T) {
	root, client, ui, _ := m5Stack(t)
	ada := mustM5Bot(t, root, "ada", "You are Ada.")
	lock, ok, err := ada.TryLockHandle()
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	srv := httptest.NewServer(webMux(ui))
	defer srv.Close()
	webc := loginWeb(t, srv, mustToken(t, ui))
	res := doJSON(t, webc, http.MethodPost, srv.URL+"/api/bots/ada/messages", `{"text":"HELLO_WEB"}`, "")
	body := readBody(t, res)
	if res.StatusCode != http.StatusAccepted || !strings.Contains(body, "queued") {
		t.Fatalf("queue %d %s", res.StatusCode, body)
	}
	entries, err := ada.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("handled while locked: %+v", entries)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	k, err := m5Kernel(ada, client)
	if err != nil {
		t.Fatal(err)
	}
	if err := proc.Drain(context.Background(), k); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(joinedEntries(t, ada), "HELLO_WEB"); n != 1 {
		t.Fatalf("HELLO_WEB count %d\n%s", n, joinedEntries(t, ada))
	}
	if !strings.Contains(joinedEntries(t, ada), "web hello") {
		t.Fatal(joinedEntries(t, ada))
	}
}

func TestBotMessagesBotAndGetsReply(t *testing.T) {
	root, client, _, _ := m5Stack(t)
	ada := mustM5Bot(t, root, "ada", "You are Ada.")
	bob := mustM5Bot(t, root, "bob", "You are Bob.")
	if err := memory.Open(root).Remember(context.Background(), memory.Fact{
		Scope: memory.ScopeBot, Tier: memory.TierProfile, BotName: "ada", WrittenBy: "ada", Text: "ADA_SECRET_FACT", Time: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	ka, err := m5Kernel(ada, client)
	if err != nil {
		t.Fatal(err)
	}
	if err := ka.Handle(context.Background(), link.TerminalEvent("u1", "MSG_BOB", time.Now())); err != nil {
		t.Fatal(err)
	}
	kb, err := m5Kernel(bob, client)
	if err != nil {
		t.Fatal(err)
	}
	if err := proc.Drain(context.Background(), kb); err != nil {
		t.Fatal(err)
	}
	ka, err = m5Kernel(ada, client)
	if err != nil {
		t.Fatal(err)
	}
	if err := proc.Drain(context.Background(), ka); err != nil {
		t.Fatal(err)
	}
	adaText := joinedEntries(t, ada)
	bobText := joinedEntries(t, bob)
	if !strings.Contains(adaText, "got reply") {
		t.Fatalf("ada:\n%s", adaText)
	}
	if !strings.Contains(bobText, "<untrusted_event") || !strings.Contains(bobText, `source="bot:ada"`) {
		t.Fatalf("bob trust:\n%s", bobText)
	}
	if strings.Contains(bobText, "ADA_SECRET_FACT") {
		t.Fatalf("ada memory leaked:\n%s", bobText)
	}
}

func TestGroupFanout(t *testing.T) {
	root, _, ui, _ := m5Stack(t)
	mustM5Bot(t, root, "ada", "You are Ada.")
	bob := mustM5Bot(t, root, "bob", "You are Bob.")
	if err := memory.Open(root).Remember(context.Background(), memory.Fact{
		Scope: memory.ScopeBot, Tier: memory.TierProfile, BotName: "bob", WrittenBy: "bob", Text: "BOB_SECRET_FACT", Time: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(webMux(ui))
	defer srv.Close()
	webc := loginWeb(t, srv, mustToken(t, ui))
	res := doJSON(t, webc, http.MethodPost, srv.URL+"/api/groups", `{"name":"room","members":["ada","bob"]}`, "")
	raw := readBody(t, res)
	if res.StatusCode != 200 {
		t.Fatalf("create %d %s", res.StatusCode, raw)
	}
	var g peer.Group
	if err := json.Unmarshal([]byte(raw), &g); err != nil || g.ID == "" {
		t.Fatal(err, raw)
	}
	res = doJSON(t, webc, http.MethodPost, srv.URL+"/api/groups/"+g.ID+"/messages", `{"text":"GROUP_PING"}`, "")
	if res.StatusCode != 200 {
		t.Fatalf("post %d %s", res.StatusCode, readBody(t, res))
	}
	view := getBody(t, webc, srv.URL+"/api/groups/"+g.ID)
	if !strings.Contains(view, "GROUP_PING") {
		t.Fatalf("group view %s", view)
	}
	for _, b := range []*bot.Bot{mustLoad(t, root, "ada"), bob} {
		box, err := inbox.Open(b.InboxPath())
		if err != nil {
			t.Fatal(err)
		}
		ev, ok := box.Head()
		if !ok || !strings.Contains(ev.Content, "GROUP_PING") || strings.Contains(ev.Content, "BOB_SECRET_FACT") {
			t.Fatalf("%s inbox %+v %v", b.Name(), ev, ok)
		}
		if ev.Source != "group:"+g.ID {
			t.Fatalf("%s source %s", b.Name(), ev.Source)
		}
	}
}

func TestPeerLoopStops(t *testing.T) {
	root, client, _, _ := m5Stack(t)
	ada := mustM5Bot(t, root, "ada", "BOTNAME=ada")
	bob := mustM5Bot(t, root, "bob", "BOTNAME=bob")
	ka, err := m5Kernel(ada, client)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := m5Kernel(bob, client)
	if err != nil {
		t.Fatal(err)
	}
	if err := ka.Handle(context.Background(), link.TerminalEvent("u1", "LOOP", time.Now())); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if err := proc.Drain(context.Background(), kb); err != nil {
			t.Fatal(err)
		}
		if err := proc.Drain(context.Background(), ka); err != nil {
			t.Fatal(err)
		}
	}
	posts := countBotPosts(t, ada) + countBotPosts(t, bob)
	if posts == 0 || posts > peer.MaxHops {
		t.Fatalf("bot posts %d", posts)
	}
	joined := joinedEntries(t, ada) + joinedEntries(t, bob)
	if !strings.Contains(joined, "hop limit") {
		t.Fatalf("missing hop limit, posts=%d\n%s", posts, joined)
	}
	if _, ok := ka.Inbox.Head(); ok {
		t.Fatal("ada inbox still pending")
	}
	if _, ok := kb.Inbox.Head(); ok {
		t.Fatal("bob inbox still pending")
	}
}

func m5Stack(t *testing.T) (string, *provider.Client, *web.Server, *httptest.Server) {
	t.Helper()
	root := t.TempDir()
	mock := httptest.NewServer(mockopenai.NewMux(t.TempDir()))
	t.Cleanup(mock.Close)
	client := &provider.Client{BaseURL: mock.URL + "/v1", APIKey: "test", Model: "mock", HTTPClient: mock.Client()}
	ui := &web.Server{
		Root:  root,
		Owner: "u1",
		NewKernel: func(b *bot.Bot) (*kernel.Kernel, error) {
			return m5Kernel(b, client)
		},
	}
	if _, _, err := ui.EnsureToken(); err != nil {
		t.Fatal(err)
	}
	return root, client, ui, mock
}

func m5Kernel(b *bot.Bot, client *provider.Client) (*kernel.Kernel, error) {
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		return nil, err
	}
	return &kernel.Kernel{
		Bot: b, Provider: client, Memory: memory.Open(b.Root), MaxSteps: 4,
		Inbox: box,
		Links: []link.Link{link.Terminal{}, link.Web{}, link.Peers{}},
		// Bot-to-bot messages do not need the owner. These tests refuse every
		// other confirmation, so any unexpected card fails the run.
		Approve: func(context.Context, string, string) (bool, error) { return false, nil },
		Budget:  compact.DefaultBudget(),
	}, nil
}

func mustM5Bot(t *testing.T, root, name, persona string) *bot.Bot {
	t.Helper()
	b, err := bot.Create(root, name, persona, []string{link.NameTerminal, link.NameWeb, link.NamePeers})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	return b
}

func mustLoad(t *testing.T, root, name string) *bot.Bot {
	t.Helper()
	b, err := bot.Load(root, name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustToken(t *testing.T, ui *web.Server) string {
	t.Helper()
	tok, _, err := ui.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func webMux(ui *web.Server) http.Handler {
	mux := http.NewServeMux()
	ui.Register(mux)
	return mux
}

func loginWeb(t *testing.T, srv *httptest.Server, token string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	raw, _ := json.Marshal(map[string]string{"token": token})
	res := doJSON(t, client, http.MethodPost, srv.URL+"/api/login", string(raw), "")
	body := readBody(t, res)
	if res.StatusCode != 200 {
		t.Fatalf("login %d %s", res.StatusCode, body)
	}
	return client
}

func doJSON(t *testing.T, client *http.Client, method, url, body, origin string) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func readBody(t *testing.T, res *http.Response) string {
	t.Helper()
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func getBody(t *testing.T, client *http.Client, url string) string {
	t.Helper()
	res, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	return readBody(t, res)
}

func rawUser(t *testing.T, b *bot.Bot, marker string) string {
	t.Helper()
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Message.Role == provider.RoleUser && strings.Contains(e.Message.Content, marker) {
			return e.Message.Content
		}
	}
	t.Fatalf("missing user %s", marker)
	return ""
}

func between(s, marker string) string {
	i := strings.Index(s, marker)
	if i < 0 {
		return ""
	}
	return s[max(0, i-80):i]
}

func countBotPosts(t *testing.T, b *bot.Bot) int {
	t.Helper()
	n := 0
	err := fsutil.ReadJSONL(b.InboxPath(), func(line []byte) error {
		if bytes.Contains(line, []byte(`"op":"post"`)) && bytes.Contains(line, []byte(`"source":"bot:`)) {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}
