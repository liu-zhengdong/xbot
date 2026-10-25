package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
)

type appsEnv struct {
	t    *testing.T
	root string
	srv  *httptest.Server
	tok  string
}

func newAppsEnv(t *testing.T) *appsEnv {
	t.Helper()
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ada", "bob"} {
		b, err := bot.Create(root, name, "persona", []string{link.NameTerminal})
		if err != nil {
			t.Fatal(err)
		}
		if err := b.SetOwners([]string{"u1"}); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(muxOf(s))
	t.Cleanup(srv.Close)
	return &appsEnv{t: t, root: root, srv: srv, tok: tok}
}

func (e *appsEnv) call(method, path, body string, auth bool) (int, []byte) {
	e.t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, rdr)
	if err != nil {
		e.t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth {
		req.AddCookie(&http.Cookie{Name: cookieName, Value: e.tok})
	}
	res, err := e.srv.Client().Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, raw
}

func (e *appsEnv) list() (int, map[string]appJSON) {
	e.t.Helper()
	code, raw := e.call("GET", "/api/apps", "", true)
	if code != 200 {
		e.t.Fatalf("list %d %s", code, raw)
	}
	var out struct {
		Connected int       `json:"connected"`
		Apps      []appJSON `json:"apps"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		e.t.Fatal(err)
	}
	m := map[string]appJSON{}
	for _, a := range out.Apps {
		m[a.ID] = a
	}
	return out.Connected, m
}

func TestAppsStoreListsCuratedCardsOnly(t *testing.T) {
	e := newAppsEnv(t)
	if code, _ := e.call("GET", "/api/apps", "", false); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated %d", code)
	}
	n, apps := e.list()
	if n != 0 {
		t.Fatalf("connected %d", n)
	}
	for _, gone := range []string{"filesystem", "localhttp", "webhook", "watch", "skills", "mcp"} {
		if _, ok := apps[gone]; ok {
			t.Fatalf("%s must not be in the store", gone)
		}
	}
	if apps["desktop"].Title != "电脑操作" || apps["browser"].Title != "浏览器" {
		t.Fatalf("local titles %+v %+v", apps["desktop"], apps["browser"])
	}
	for _, id := range []string{"fetch", "playwright", "context7", "telegram", "lark"} {
		if apps[id].State != "available" {
			t.Fatalf("%s %+v", id, apps[id])
		}
	}
	for _, id := range []string{"gmail", "gdrive", "gcalendar"} {
		if apps[id].State != "soon" {
			t.Fatalf("%s %+v", id, apps[id])
		}
		if code, _ := e.call("POST", "/api/apps", `{"id":"`+id+`"}`, true); code != http.StatusConflict {
			t.Fatalf("soon app %s installed: %d", id, code)
		}
	}
	banned := regexp.MustCompile(`(?i)MCP|stdio|\benv\b|npx|uvx|command|args|transport|secret_envs|mcp\.json|JSON|CLI|桌面`)
	for _, a := range apps {
		if banned.MatchString(a.Title + a.Summary) {
			t.Fatalf("engineering word in %+v", a)
		}
	}
}

func TestAppsInstallIsGlobalAndIdempotent(t *testing.T) {
	e := newAppsEnv(t)
	for i := 0; i < 2; i++ { // a second click changes nothing
		if code, raw := e.call("POST", "/api/apps", `{"id":"fetch"}`, true); code != 200 {
			t.Fatalf("install %d %s", code, raw)
		}
		if code, raw := e.call("POST", "/api/apps", `{"id":"desktop"}`, true); code != 200 {
			t.Fatalf("install %d %s", code, raw)
		}
	}
	n, apps := e.list()
	if n != 2 || apps["fetch"].State != "connected" || apps["desktop"].State != "connected" {
		t.Fatalf("%d %+v", n, apps)
	}
	global, _ := link.LoadMCP(bot.GlobalMCPPath(e.root))
	if len(global) != 1 || global[0].Name != "fetch" {
		t.Fatalf("global %+v", global)
	}
	for _, name := range []string{"ada", "bob"} {
		b, err := bot.Load(e.root, name)
		if err != nil {
			t.Fatal(err)
		}
		if own, _ := link.LoadMCP(b.MCPPath()); len(own) != 0 {
			t.Fatalf("%s got a per-bot copy: %+v", name, own)
		}
		if !b.Grants(link.NameDesktop) {
			t.Fatalf("%s lacks the global desktop grant", name)
		}
		if all, _ := link.LoadMCPAll(b.GlobalMCPPath(), b.MCPPath()); len(all) != 1 {
			t.Fatalf("%s sees %+v", name, all)
		}
		if b.Config().Links[0] != link.NameTerminal || len(b.Config().Links) != 1 {
			t.Fatalf("%s config changed: %v", name, b.Config().Links)
		}
	}
	// removing is global too, and removing twice is fine
	for i := 0; i < 2; i++ {
		for _, id := range []string{"fetch", "desktop"} {
			if code, raw := e.call("DELETE", "/api/apps/"+id, "", true); code != 200 {
				t.Fatalf("remove %s %d %s", id, code, raw)
			}
		}
	}
	if n, _ := e.list(); n != 0 {
		t.Fatalf("connected after remove %d", n)
	}
	b, _ := bot.Load(e.root, "bob")
	if b.Grants(link.NameDesktop) {
		t.Fatal("bob still granted desktop")
	}
	if all, _ := link.LoadMCPAll(b.GlobalMCPPath(), b.MCPPath()); len(all) != 0 {
		t.Fatalf("bob still sees %+v", all)
	}
}

func TestAppsKeyConnectorsNeverTakeKeysOverHTTP(t *testing.T) {
	e := newAppsEnv(t)
	for _, id := range []string{"telegram", "lark"} {
		if code, _ := e.call("POST", "/api/apps", `{"id":"`+id+`"}`, true); code != http.StatusConflict {
			t.Fatalf("%s %d", id, code)
		}
		// a key in the body is refused outright, not stored
		if code, _ := e.call("POST", "/api/apps", `{"id":"`+id+`","token":"sk-secret-123"}`, true); code != http.StatusBadRequest {
			t.Fatalf("%s with key %d", id, code)
		}
	}
	if _, err := os.Stat(bot.AppsPath(e.root)); err == nil {
		t.Fatal("a key connector was granted without its key")
	}
	if code, _ := e.call("POST", "/api/apps", `{"id":"nope"}`, true); code != http.StatusNotFound {
		t.Fatalf("unknown %d", code)
	}
	// A bot that took the key through chat makes the connector show as connected for the owner; removing drops it everywhere.
	ada, _ := bot.Load(e.root, "ada")
	if err := ada.GrantLink(link.NameTelegram); err != nil {
		t.Fatal(err)
	}
	if _, apps := e.list(); apps["telegram"].State != "connected" || apps["telegram"].Scope != keyPerBotNote || strings.Contains(apps["telegram"].Summary, "分别保存") {
		t.Fatalf("connected key app must say the key stays per Bot: %+v", apps["telegram"])
	}
	if _, apps := e.list(); apps["fetch"].Scope != "" || apps["lark"].Scope != keyPerBotNote {
		t.Fatal("per-Bot note belongs on key apps only")
	}
	if code, raw := e.call("DELETE", "/api/apps/telegram", "", true); code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	ada, _ = bot.Load(e.root, "ada")
	if ada.Grants(link.NameTelegram) {
		t.Fatal("telegram still granted")
	}
}

func TestAppsCustomLayer(t *testing.T) {
	e := newAppsEnv(t)
	cases := []struct {
		body string
		want int
	}{
		{`{"name":"notes"}`, 400},
		{`{"name":"notes","program":"/bin/echo","url":"https://example.com/mcp"}`, 400},
		{`{"name":"fetch","program":"/bin/echo"}`, 409},
		{`{"name":"notes","url":"ftp://example.com"}`, 400},
		{`{"name":"notes","url":"https://example.com/mcp"}`, 200},
		{`{"name":"tool","program":"/bin/echo","options":["a","b"]}`, 200},
	}
	for _, c := range cases {
		if code, raw := e.call("POST", "/api/apps/custom", c.body, true); code != c.want {
			t.Fatalf("%s: %d %s", c.body, code, raw)
		}
	}
	n, apps := e.list()
	if n != 2 || !apps["notes"].Custom || apps["notes"].State != "connected" {
		t.Fatalf("%d %+v", n, apps)
	}
	if code, _ := e.call("DELETE", "/api/apps/notes", "", true); code != 200 {
		t.Fatal(code)
	}
	if code, _ := e.call("DELETE", "/api/apps/notes", "", true); code != http.StatusNotFound {
		t.Fatalf("second remove %d", code)
	}
}

// The store routes carry no bot name, so what the sidebar has selected cannot change them.
func TestAppsRoutesNameNoBot(t *testing.T) {
	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(src), "\n") {
		if strings.Contains(line, `"/api/apps`) && strings.Contains(line, "{name}") {
			t.Fatalf("store route bound to a bot: %s", line)
		}
	}
}
