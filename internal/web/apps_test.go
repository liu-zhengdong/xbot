package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/browser"
	"xbot/internal/link"
)

type appsEnv struct {
	t      *testing.T
	root   string
	srv    *httptest.Server
	tok    string
	server *Server
}

func newAppsEnv(t *testing.T) *appsEnv {
	t.Helper()
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1", appsProbe: func(context.Context, link.App, []string) error { return nil }}
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
	return &appsEnv{t: t, root: root, srv: srv, tok: tok, server: s}
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
	// Our own abilities are not store cards: files, events, skills, organs.
	for _, gone := range []string{"filesystem", "localhttp", "webhook", "watch", "skills", "mcp"} {
		if _, ok := apps[gone]; ok {
			t.Fatalf("%s must not be in the store", gone)
		}
	}
	for _, o := range link.Organs() {
		if _, ok := apps[o.ID]; ok {
			t.Fatalf("organ %s must not be in the store", o.ID)
		}
	}
	// A retired catalog row is not a card either.
	if _, ok := apps["fetch"]; ok || !link.RetiredServer("fetch") {
		t.Fatal("retired fetch must not be in the store")
	}
	// The browser depends on the owner's Chrome: a Link card.
	for _, id := range []string{"browser", "playwright", "context7", "telegram", "lark"} {
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
		if code, raw := e.call("POST", "/api/apps", `{"id":"playwright"}`, true); code != 200 {
			t.Fatalf("install %d %s", code, raw)
		}
	}
	// organs are on from boot, never installed through the store
	for _, o := range link.Organs() {
		if code, _ := e.call("POST", "/api/apps", `{"id":"`+o.ID+`"}`, true); code != http.StatusNotFound {
			t.Fatalf("%s installed from the store: %d", o.ID, code)
		}
	}
	n, apps := e.list()
	if n != 1 || apps["playwright"].State != "connected" {
		t.Fatalf("%d %+v", n, apps)
	}
	global, _ := link.LoadMCP(bot.GlobalMCPPath(e.root))
	if len(global) != 1 || global[0].Name != "playwright" {
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
		if all, _ := link.LoadMCPAll(b.GlobalMCPPath(), b.MCPPath()); len(all) != 1 {
			t.Fatalf("%s sees %+v", name, all)
		}
		if b.Config().Links[0] != link.NameTerminal || len(b.Config().Links) != 1 {
			t.Fatalf("%s config changed: %v", name, b.Config().Links)
		}
	}
	// removing is global too, and removing twice is fine
	for i := 0; i < 2; i++ {
		for _, id := range []string{"playwright"} {
			if code, raw := e.call("DELETE", "/api/apps/"+id, "", true); code != 200 {
				t.Fatalf("remove %s %d %s", id, code, raw)
			}
		}
	}
	if n, _ := e.list(); n != 0 {
		t.Fatalf("connected after remove %d", n)
	}
	b, _ := bot.Load(e.root, "bob")
	if all, _ := link.LoadMCPAll(b.GlobalMCPPath(), b.MCPPath()); len(all) != 0 {
		t.Fatalf("bob still sees %+v", all)
	}
}

func TestAppsKeyConnectorsNeedTheirKeys(t *testing.T) {
	e := newAppsEnv(t)
	for _, id := range []string{"telegram", "lark"} {
		if code, _ := e.call("POST", "/api/apps", `{"id":"`+id+`"}`, true); code != http.StatusBadRequest {
			t.Fatalf("%s %d", id, code)
		}
		// a key under any other field is refused outright, not stored
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
	// The layer asks in plain words; variable names never reach the page.
	if _, apps := e.list(); strings.Join(apps["context7"].Fields, ",") != "API 密钥" || strings.Join(apps["lark"].Fields, ",") != "应用密钥" || len(apps["playwright"].Fields) != 0 {
		t.Fatalf("fields %+v %+v", apps["context7"], apps["lark"])
	}
	if _, raw := e.call("GET", "/api/apps", "", true); strings.Contains(string(raw), "_KEY") || strings.Contains(string(raw), "_TOKEN") || strings.Contains(string(raw), "_SECRET") {
		t.Fatal("variable names on the page")
	}
	if _, apps := e.list(); apps["playwright"].Scope != "" || apps["lark"].Scope != keyPerBotNote {
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
		{`{"name":"playwright","program":"/bin/echo"}`, 409},
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

// Context7 needs its key: the store hands it to the chat card, a click over HTTP
// writes nothing, and an entry without the key never reads as connected.
func TestAppsContext7NeedsKey(t *testing.T) {
	t.Setenv("CONTEXT7_API_KEY", "") // the shell's own key must not make it look connected
	e := newAppsEnv(t)
	if _, apps := e.list(); !apps["context7"].Card || apps["context7"].State != "available" || apps["playwright"].Card {
		t.Fatalf("card flags %+v %+v", apps["context7"], apps["playwright"])
	}
	for i := 0; i < 2; i++ { // a repeated click is the same refusal
		code, raw := e.call("POST", "/api/apps", `{"id":"context7"}`, true)
		if code != http.StatusBadRequest {
			t.Fatalf("context7 install %d %s", code, raw)
		}
	}
	if code, _ := e.call("POST", "/api/apps", `{"id":"context7","CONTEXT7_API_KEY":"ctx7sk-abc"}`, true); code != http.StatusBadRequest {
		t.Fatalf("key in body %d", code)
	}
	if _, err := os.Stat(bot.GlobalMCPPath(e.root)); err == nil {
		t.Fatal("context7 was written without its key")
	}
	if n, apps := e.list(); n != 0 || apps["context7"].State != "available" {
		t.Fatalf("after refusal %d %+v", n, apps["context7"])
	}
	// An entry left keyless by the old one-click add does not count as connected.
	path := bot.GlobalMCPPath(e.root)
	if err := link.AddMCP(path, link.MCPServer{Name: "context7", Command: "npx", Args: []string{"-y", "@upstash/context7-mcp"}}); err != nil {
		t.Fatal(err)
	}
	if n, apps := e.list(); n != 0 || apps["context7"].State != "available" {
		t.Fatalf("keyless entry shown %d %+v", n, apps["context7"])
	}
	if err := link.RemoveMCP(path, "context7"); err != nil {
		t.Fatal(err)
	}
	if err := link.EnableConnector(path, "context7"); err != nil {
		t.Fatal(err)
	}
	// The entry names the key but nobody holds a value: not connected.
	if n, apps := e.list(); n != 0 || apps["context7"].State != "available" {
		t.Fatalf("name without a value shown %d %+v", n, apps["context7"])
	}
	// An empty value is no key either.
	ada, _ := bot.Load(e.root, "ada")
	if err := link.WriteSecrets(ada.SecretPath(), map[string]string{"CONTEXT7_API_KEY": "  "}); err != nil {
		t.Fatal(err)
	}
	if n, apps := e.list(); n != 0 || apps["context7"].State != "available" {
		t.Fatalf("empty value shown %d %+v", n, apps["context7"])
	}
	// A Bot holding a real value (where links install puts it) makes it connected.
	if err := link.WriteSecrets(ada.SecretPath(), map[string]string{"CONTEXT7_API_KEY": "ctx7sk-abc"}); err != nil {
		t.Fatal(err)
	}
	if n, apps := e.list(); n != 1 || apps["context7"].State != "connected" {
		t.Fatalf("keyed entry %d %+v", n, apps["context7"])
	}
	if code, raw := e.call("GET", "/api/apps", "", true); code != 200 || strings.Contains(string(raw), "ctx7sk-abc") {
		t.Fatalf("key value in the list %d", code)
	}
	for i := 0; i < 2; i++ {
		if code, raw := e.call("DELETE", "/api/apps/context7", "", true); code != 200 {
			t.Fatalf("remove %d %s", code, raw)
		}
	}
	if all, _ := link.LoadMCP(path); len(all) != 0 {
		t.Fatalf("left %+v", all)
	}
	if got, _ := link.ReadSecrets(ada.SecretPath()); got["CONTEXT7_API_KEY"] != "" {
		t.Fatal("store remove left the key with the Bot")
	}
}

// The process environment is the spawn fallback, so a non-empty value there
// counts the same as a Bot's stored key.
func TestAppsContext7KeyFromEnvironment(t *testing.T) {
	t.Setenv("CONTEXT7_API_KEY", "ctx7sk-env")
	e := newAppsEnv(t)
	if err := link.EnableConnector(bot.GlobalMCPPath(e.root), "context7"); err != nil {
		t.Fatal(err)
	}
	if n, apps := e.list(); n != 1 || apps["context7"].State != "connected" {
		t.Fatalf("env key %d %+v", n, apps["context7"])
	}
	// A blank stored value masks the environment at start (the Bot's store is
	// read first), so with every Bot blank the store must not say connected.
	var spawnSees []link.MCPServer
	for _, name := range []string{"ada", "bob"} {
		b, _ := bot.Load(e.root, name)
		if err := link.WriteSecrets(b.SecretPath(), map[string]string{"CONTEXT7_API_KEY": "   "}); err != nil {
			t.Fatal(err)
		}
		spawnSees = append(spawnSees, link.MCPServer{Name: "context7", SecretEnvs: []string{"CONTEXT7_API_KEY"}, Secret: link.SecretLookup(b.SecretPath())})
	}
	for _, sv := range spawnSees {
		if sv.HasSecretValues(sv.SecretEnvs) {
			t.Fatal("a blank stored value should leave the server without a key")
		}
	}
	if n, apps := e.list(); n != 0 || apps["context7"].State != "available" {
		t.Fatalf("blank store plus env shown %d %+v", n, apps["context7"])
	}
	// Once one Bot has no stored value, its server falls back to the environment.
	bob, _ := bot.Load(e.root, "bob")
	if err := link.DropSecrets(bob.SecretPath(), []string{"CONTEXT7_API_KEY"}); err != nil {
		t.Fatal(err)
	}
	if !spawnSees[1].HasSecretValues(spawnSees[1].SecretEnvs) {
		t.Fatal("bob's server should read the environment")
	}
	if n, apps := e.list(); n != 1 || apps["context7"].State != "connected" {
		t.Fatalf("env fallback %d %+v", n, apps["context7"])
	}
}

func TestAppsProbeBeforeCommit(t *testing.T) {
	e := newAppsEnv(t)
	e.server.appsProbe = func(_ context.Context, _ link.App, _ []string) error { return errors.New("private fixture error") }
	for _, body := range []string{`{"id":"playwright"}`, `{"id":"context7","secrets":["fixture-key"]}`} {
		code, raw := e.call("POST", "/api/apps", body, true)
		if code != 422 || strings.Contains(string(raw), "fixture") {
			t.Fatalf("failure %d %s", code, raw)
		}
	}
	if n, rows := e.list(); n != 0 || rows["context7"].State != "available" || rows["playwright"].State != "available" {
		t.Fatal("failed probe installed an app")
	}
	if _, err := os.Stat(bot.GlobalMCPPath(e.root)); !os.IsNotExist(err) {
		t.Fatal("failed probe wrote configuration")
	}
	ada, _ := bot.Load(e.root, "ada")
	if keys, _ := link.ReadSecrets(ada.SecretPath()); len(keys) != 0 {
		t.Fatal("failed probe stored credentials")
	}
	e.server.appsProbe = func(context.Context, link.App, []string) error { return fmt.Errorf("probe: %w", browser.ErrNoChrome) }
	if code, raw := e.call("POST", "/api/apps", `{"id":"browser"}`, true); code != 422 || !strings.Contains(string(raw), "没找到 Chrome") {
		t.Fatalf("no chrome %d %s", code, raw)
	}
	e.server.appsProbe = func(_ context.Context, a link.App, values []string) error {
		if a.ID != "context7" || len(values) != 1 || values[0] != "fixture-key" {
			return errors.New("bad probe input")
		}
		return nil
	}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, _ := e.call("POST", "/api/apps", `{"id":"context7","secrets":["fixture-key"]}`, true)
			codes <- code
		}()
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != 200 {
			t.Fatalf("repeat install: %d", code)
		}
	}
	if n, rows := e.list(); n != 1 || rows["context7"].State != "connected" {
		t.Fatal("verified app missing")
	}
	if _, raw := e.call("GET", "/api/apps", "", true); strings.Contains(string(raw), "fixture-key") {
		t.Fatal("credential exposed in list")
	}
}

// An old global mcp.json entry for a retired catalog row is neither a custom
// card nor counted as connected.
func TestAppsHideRetiredServer(t *testing.T) {
	e := newAppsEnv(t)
	if err := link.AddMCP(bot.GlobalMCPPath(e.root), link.MCPServer{Name: "fetch", Command: "uvx", Args: []string{"mcp-server-fetch"}}); err != nil {
		t.Fatal(err)
	}
	n, apps := e.list()
	if _, ok := apps["fetch"]; ok || n != 0 {
		t.Fatalf("retired fetch shown: %d %+v", n, apps["fetch"])
	}
}

// The browser installs from the store as a native grant for every Bot.
func TestAppsInstallBrowserGrantsGlobally(t *testing.T) {
	e := newAppsEnv(t)
	if code, raw := e.call("POST", "/api/apps", `{"id":"browser"}`, true); code != 200 {
		t.Fatalf("install %d %s", code, raw)
	}
	n, apps := e.list()
	if n != 1 || apps["browser"].State != "connected" {
		t.Fatalf("%d %+v", n, apps["browser"])
	}
	global, err := bot.GlobalLinks(e.root)
	if err != nil || !slices.Contains(global, link.NameBrowser) {
		t.Fatalf("global links %v %v", global, err)
	}
}
