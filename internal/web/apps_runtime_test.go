package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/kernel"
	"xbot/internal/link"
)

func TestAppsExposeMCPRuntimeWithoutProbing(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "persona", []string{link.NameTerminal})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	spec := link.MCPServer{Name: "playwright", Command: "/missing/mcp-runtime-fixture"}
	if err := link.AddMCP(bot.GlobalMCPPath(root), spec); err != nil {
		t.Fatal(err)
	}
	m := link.NewMCP(spec)
	defer m.Close()
	k := &kernel.Kernel{Bot: b, Links: []link.Link{m}}
	s := &Server{Root: root, Owner: "u1", kernels: map[string]*kernel.Kernel{"ada": k}}
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
	if row.LoadID != "mcp:playwright" || row.Runtime["ada"].State != "configured" || m.Status().State != "configured" {
		t.Fatalf("GET probed process or hid configured state: %+v", row)
	}
	if _, err := m.LoadTools(context.Background()); err == nil {
		t.Fatal("missing executable succeeded")
	}
	row = read()
	if st := row.Runtime["ada"]; st.State != "error" || !strings.Contains(st.Error, "mcp-runtime-fixture") {
		t.Fatalf("failure missing from API: %+v", row)
	}
	// A changed configuration must not inherit the old instance's failure.
	if err := link.RemoveMCP(bot.GlobalMCPPath(root), "playwright"); err != nil {
		t.Fatal(err)
	}
	spec.Command = "/different/fixture"
	if err := link.AddMCP(bot.GlobalMCPPath(root), spec); err != nil {
		t.Fatal(err)
	}
	if st := read().Runtime["ada"]; st.State != "configured" || st.Error != "" {
		t.Fatalf("stale status: %+v", st)
	}
	// Successful discovery is reported for this Bot only.
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	if err := link.RemoveMCP(bot.GlobalMCPPath(root), "playwright"); err != nil {
		t.Fatal(err)
	}
	spec = link.MCPServer{Name: "playwright", URL: rpc.URL}
	if err := link.AddMCP(bot.GlobalMCPPath(root), spec); err != nil {
		t.Fatal(err)
	}
	ready := link.NewMCP(spec)
	defer ready.Close()
	k.Links = []link.Link{ready}
	if _, err := ready.LoadTools(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := read().Runtime["ada"]; st.State != "ready" || st.ToolCount != 1 {
		t.Fatalf("success missing: %+v", st)
	}

}
