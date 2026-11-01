//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type cliBotForkMemoryAttempt struct{ Bot, Key string }
type cliBotForkReply struct {
	Name            string
	Created         bool
	CreateStage     string `json:"create_stage"`
	Confirmed       []string
	HandoffParts    int                      `json:"handoff_parts"`
	HandoffTotal    int                      `json:"handoff_total"`
	UncertainMemory *cliBotForkMemoryAttempt `json:"uncertain_memory"`
	GroupID         string                   `json:"group_id"`
	Stage, Error    string
}
type cliBotForkArchiveRequest struct {
	stubRequest
	Tools  []json.RawMessage `json:"tools"`
	Stream bool              `json:"stream"`
	Model  string            `json:"model"`
}

// The provider alone is scripted: original compact.LLM must actually call Chat
// without tools. No Compactor, Kernel, memory service or source context is made.
type cliBotForkModel struct {
	base    *mcpRuntimeModel
	server  *httptest.Server
	mu      sync.Mutex
	archive []cliBotForkArchiveRequest
	raw     [][]byte
	handoff string
	entered chan struct{}
	release <-chan struct{}
}

func newCLIBotForkModel(t *testing.T, handoff string) *cliBotForkModel {
	t.Helper()
	m := &cliBotForkModel{base: &mcpRuntimeModel{scripts: map[string]*mcpRuntimeScript{}}, handoff: handoff}
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
		if len(req.Messages) > 0 && req.Messages[0].Role == "system" && strings.HasPrefix(req.Messages[0].Content, compactionPrefix) {
			m.mu.Lock()
			m.archive = append(m.archive, req)
			body, entered, release := m.handoff, m.entered, m.release
			m.mu.Unlock()
			if entered != nil {
				select {
				case entered <- struct{}{}:
				default:
				}
			}
			if release != nil {
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
			}
			// Original OpenAI Chat streams on the wire even with nil onDelta.
			if req.Stream {
				startSSE(w)
				writeChunk(w, body)
				writeData(w, map[string]any{"choices": []any{}, "usage": map[string]int{"prompt_tokens": 20, "completion_tokens": 10, "total_tokens": 30}})
				fmt.Fprint(w, "data: [DONE]\n\n")
				flush(w)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "scripted-fork-archive", "object": "chat.completion", "choices": []any{map[string]any{"index": 0, "message": map[string]string{"role": "assistant", "content": body}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 20, "completion_tokens": 10, "total_tokens": 30}})
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		m.base.handle(w, r)
	}))
	t.Cleanup(m.server.Close)
	return m
}
