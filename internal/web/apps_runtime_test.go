package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/config"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/mcp"
)

func TestAppsExposeMCPRuntimeWithoutProbing(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "persona", []string{inbox.NameTerminal})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	options := mcp.Options{IsProviderKey: config.ProviderKey}
	spec := mcp.Spec{Name: "playwright", Command: "/missing/mcp-runtime-fixture"}
	if err := mcp.AddMCP(bot.GlobalMCPPath(root), spec, options); err != nil {
		t.Fatal(err)
	}
	s := &Server{Root: root, Owner: "u1", kernels: map[string]*kernelEntry{"ada": {}}}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	handler := muxOf(s)
	read := func() appJSON {
		t.Helper()
		req := httptest.NewRequest("GET", "/api/apps", nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		var payload struct {
			Apps []appJSON `json:"apps"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		for _, row := range payload.Apps {
			if row.ID == "playwright" {
				return row
			}
		}
		t.Fatal("missing app")
		return appJSON{}
	}
	row := read()
	if row.LoadID != "mcp:playwright" || row.Runtime["ada"].State != "configured" {
		t.Fatalf("GET created a runtime or hid configured state: %+v", row)
	}
	owner := mcp.NewManager(context.Background(), options)
	defer owner.Close()
	ref, err := owner.Bind(mcp.Binding{Key: "ada", ReadConfig: func() ([]mcp.Spec, error) { return mcp.LoadMCPAll(options, b.GlobalMCPPath(), b.MCPPath()) }})
	if err != nil {
		t.Fatal(err)
	}
	s.kernels["ada"].Resources = &kernel.BotResources{MCP: ref}
	if _, err := ref.LoadTools(context.Background(), "playwright"); err == nil {
		t.Fatal("missing executable succeeded")
	}
	row = read()
	if st := row.Runtime["ada"]; st.State != "error" || !strings.Contains(st.Error, "mcp-runtime-fixture") {
		t.Fatalf("failure missing from API: %+v", row)
	}
	// The GET must not return an older effective spec's error as the status of
	// a newly saved configuration, or reconcile/probe it as a side effect.
	if err := mcp.RemoveMCP(bot.GlobalMCPPath(root), "playwright", options); err != nil {
		t.Fatal(err)
	}
	spec.Command = "/different/fixture"
	if err := mcp.AddMCP(bot.GlobalMCPPath(root), spec, options); err != nil {
		t.Fatal(err)
	}
	if st := read().Runtime["ada"]; st.State != "configured" || st.Error != "" {
		t.Fatalf("stale status: %+v", st)
	}
	if st := ref.Status("playwright"); !strings.Contains(st.Error, "mcp-runtime-fixture") {
		t.Fatal("GET reconciled the runtime", st)
	}
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&msg)
		if len(msg.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		result := json.RawMessage(`{}`)
		if msg.Method == "tools/list" {
			result = json.RawMessage(`{"tools":[{"name":"fixture_tool"}]}`)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": result})
	}))
	defer rpc.Close()
	if err := mcp.RemoveMCP(bot.GlobalMCPPath(root), "playwright", options); err != nil {
		t.Fatal(err)
	}
	spec = mcp.Spec{Name: "playwright", URL: rpc.URL}
	if err := mcp.AddMCP(bot.GlobalMCPPath(root), spec, options); err != nil {
		t.Fatal(err)
	}
	if err := ref.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := ref.LoadTools(context.Background(), "playwright"); err != nil {
		t.Fatal(err)
	}
	if st := read().Runtime["ada"]; st.State != "ready" || st.ToolCount != 1 {
		t.Fatalf("success missing: %+v", st)
	}
}
