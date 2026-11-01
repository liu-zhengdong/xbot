package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The HTTP exchange itself must not occupy the stdio frame gate. A second
// request's cancellation must return while the first request is still blocked.
func TestMCPConcurrentHTTPCancelKeepsOtherCall(t *testing.T) {
	enteredA, enteredB := make(chan struct{}), make(chan json.RawMessage, 1)
	releaseA := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(releaseA) })
	hints := make(chan json.RawMessage, 2)
	var hintCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "decode", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "concurrent-session")
		var result any = map[string]any{}
		switch req.Method {
		case "notifications/initialized":
			w.WriteHeader(http.StatusNoContent)
			return
		case "initialize":
			result = map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "wait", "inputSchema": map[string]any{"type": "object"}}}}
		case "notifications/cancelled":
			var p struct {
				RequestID json.RawMessage `json:"requestId"`
			}
			_ = json.Unmarshal(req.Params, &p)
			hintCount.Add(1)
			hints <- p.RequestID
			w.WriteHeader(http.StatusNoContent)
			return
		case "tools/call":
			var p struct {
				Arguments struct {
					Who string `json:"who"`
				} `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			if p.Arguments.Who == "A" {
				close(enteredA)
				select {
				case <-releaseA:
				case <-r.Context().Done():
					return
				}
			} else {
				enteredB <- req.ID
				<-r.Context().Done()
				return
			}
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "A survived"}}}
		default:
			http.Error(w, "unknown method", 400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer srv.Close()
	// Release A before closing the server even when a regression fails early.
	defer once.Do(func() { close(releaseA) })
	m := NewSession(Spec{Name: "concurrent", URL: srv.URL}, Options{})
	defer m.Close()
	list, err := m.LoadTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatal(len(list))
	}
	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()
	aDone, bDone := make(chan error, 1), make(chan error, 1)
	go func() { _, err := m.CallTool(ctxA, list[0].Name, json.RawMessage(`{"who":"A"}`)); aDone <- err }()
	select {
	case <-enteredA:
	case <-time.After(time.Second):
		t.Fatal("A did not reach server")
	}
	go func() { _, err := m.CallTool(ctxB, list[0].Name, json.RawMessage(`{"who":"B"}`)); bDone <- err }()
	var bID json.RawMessage
	select {
	case bID = <-enteredB:
	case <-time.After(time.Second): /* old global write lock prevents admission */
	}
	cancelB()
	select {
	case err := <-bDone:
		if err == nil {
			t.Fatal("B cancellation succeeded unexpectedly")
		}
	case <-time.After(time.Second):
		t.Fatal("B cancellation waited for A's HTTP response")
	}
	if len(bID) == 0 {
		t.Fatal("concurrent B never reached the same session")
	}
	select {
	case err := <-aDone:
		t.Fatalf("B cancellation ended A: %v", err)
	default:
	}
	select {
	case id := <-hints:
		if string(id) != string(bID) {
			t.Fatalf("cancel id=%s, original B id=%s", id, bID)
		}
	case <-time.After(time.Second):
		t.Fatal("no bounded cancel hint for original request")
	}
	once.Do(func() { close(releaseA) })
	select {
	case err := <-aDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("A did not finish")
	}
	if hintCount.Load() != 1 {
		t.Fatalf("cancel hint count=%d", hintCount.Load())
	}
}
