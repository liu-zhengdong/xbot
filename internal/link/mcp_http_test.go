package link

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStreamableHTTPRoundTrip(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "gh-secret")
	t.Setenv("XBOT_API_KEY", "sk-provider")
	SetProviderKey(func(name string) bool { return name == "XBOT_API_KEY" })
	defer SetProviderKey(nil)

	note := `{"jsonrpc":"2.0","method":"notifications/message","params":{"text":"hello-from-http"}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("XBOT_API_KEY") != "" || strings.Contains(r.Header.Get("Authorization"), "sk-provider") {
			http.Error(w, "provider key leaked", http.StatusInternalServerError)
			return
		}
		if r.Method == http.MethodGet {
			if r.Header.Get("Mcp-Session-Id") != "sess-1" {
				http.Error(w, "no session", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: "+note+"\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			<-r.Context().Done()
			return
		}
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if msg.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if r.Header.Get("GITHUB_TOKEN") != "gh-secret" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Mcp-Session-Id", "sess-1")
		switch msg.Method {
		case "initialize":
			writeRPC(w, msg.ID, map[string]any{"protocolVersion": mcpProtocol})
		case "tools/list":
			w.Header().Set("Content-Type", "text/event-stream")
			payload := rpcLine(msg.ID, map[string]any{
				"tools": []any{map[string]any{
					"name":        "ping",
					"description": "ping",
					"inputSchema": map[string]any{"type": "object"},
					"annotations": map[string]any{"readOnlyHint": true},
				}},
			})
			_, _ = io.WriteString(w, "data: "+payload+"\n\n")
		case "tools/call":
			writeRPC(w, msg.ID, map[string]any{
				"content": []any{map[string]any{"type": "text", "text": "pong"}},
			})
		default:
			http.Error(w, "unexpected "+msg.Method, http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	m := NewMCP(MCPServer{
		Name:       "docs",
		URL:        srv.URL,
		SecretEnvs: []string{"GITHUB_TOKEN", "XBOT_API_KEY"},
	})
	defer m.Close()
	list := m.Tools()
	if len(list) != 1 || list[0].Name() != "mcp_docs_ping" {
		got := []string{}
		for _, tool := range list {
			got = append(got, tool.Name())
		}
		t.Fatalf("tools %v", got)
	}
	out, err := list[0].Run(context.Background(), []byte(`{}`))
	if err != nil || !strings.Contains(out, "pong") {
		t.Fatalf("%q %v", out, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan string, 1)
	go func() {
		_ = m.Run(ctx, func(ev Event) error {
			got <- ev.Content
			cancel()
			return nil
		})
	}()
	select {
	case text := <-got:
		if !strings.Contains(text, "hello-from-http") {
			t.Fatal(text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no http notification")
	}
}

func writeRPC(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
}

func rpcLine(id json.RawMessage, result any) string {
	raw, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
	return string(raw)
}
