//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Only the provider is scripted; production compact.LLM still owns Chat,
// cancellation and usage. Modes select one actual HTTP provider response.
type cliCompactModel struct {
	base          *mcpRuntimeModel
	server        *httptest.Server
	mu            sync.Mutex
	requests      []cliBotForkArchiveRequest
	raw           [][]byte
	mode, summary string
	entered       chan struct{}
	release       <-chan struct{}
	canceled      chan struct{}
	cancelOnce    sync.Once
}

func newCLICompactModel(t *testing.T, mode, summary string) *cliCompactModel {
	t.Helper()
	m := &cliCompactModel{base: &mcpRuntimeModel{scripts: map[string]*mcpRuntimeScript{}}, mode: mode, summary: summary, entered: make(chan struct{}, 1), canceled: make(chan struct{})}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad scripted request", 400)
			return
		}
		r.Body.Close()
		var req cliBotForkArchiveRequest
		if json.Unmarshal(raw, &req) != nil {
			http.Error(w, "bad scripted JSON", 400)
			return
		}
		m.mu.Lock()
		m.raw = append(m.raw, bytes.Clone(raw))
		m.mu.Unlock()
		if len(req.Messages) > 0 && req.Messages[0].Role == "system" && len(req.Messages[0].Content) >= len(compactionPrefix) && req.Messages[0].Content[:len(compactionPrefix)] == compactionPrefix {
			m.mu.Lock()
			m.requests = append(m.requests, req)
			release := m.release
			m.mu.Unlock()
			select {
			case m.entered <- struct{}{}:
			default:
			}
			if release != nil {
				select {
				case <-release:
				case <-r.Context().Done():
					m.cancelOnce.Do(func() { close(m.canceled) })
					return
				}
			}
			if m.mode == "error" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "COMPACT_SCRIPTED_PROVIDER_FAILURE", "type": "invalid_request_error"}})
				return
			}
			body := m.summary
			if m.mode == "empty" {
				body = "  \n "
			}
			if req.Stream {
				startSSE(w)
				writeChunk(w, body)
				writeData(w, map[string]any{"choices": []any{}, "usage": map[string]int{"prompt_tokens": 20, "completion_tokens": 10, "total_tokens": 30}})
				fmt.Fprint(w, "data: [DONE]\n\n")
				flush(w)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "scripted-compact", "object": "chat.completion", "choices": []any{map[string]any{"index": 0, "message": map[string]string{"role": "assistant", "content": body}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 20, "completion_tokens": 10, "total_tokens": 30}})
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		m.base.handle(w, r)
	}))
	t.Cleanup(m.server.Close)
	return m
}
