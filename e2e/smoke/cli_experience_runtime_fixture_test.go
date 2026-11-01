//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type cliExperienceRecordReply struct {
	ID       int64
	From, To int
	Recorded *bool
	Error    string
}
type cliExperienceSearchReply struct {
	Text, File, Access string
	Bytes              int64
	Error              string
}

// Only the provider is scripted. Domain calls, current source, transcript,
// SQLite, environment sampling, and native detail remain the original runtime.
type cliExperienceModel struct {
	base   *mcpRuntimeModel
	server *httptest.Server
	mu     sync.Mutex
	raw    [][]byte
}

func newCLIExperienceModel(t *testing.T) *cliExperienceModel {
	t.Helper()
	m := &cliExperienceModel{base: &mcpRuntimeModel{scripts: map[string]*mcpRuntimeScript{}}}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad scripted request", 400)
			return
		}
		r.Body.Close()
		m.mu.Lock()
		m.raw = append(m.raw, bytes.Clone(raw))
		m.mu.Unlock()
		// Preserve the actual wire above. The existing scripted selector skips
		// environment notes but not the real interjection note that follows the
		// newest owner input. Only its private selection copy omits that note.
		var request stubRequest
		if json.Unmarshal(raw, &request) == nil && strings.HasPrefix(lastUser(request), "<owner_interjected>") {
			var wire map[string]json.RawMessage
			var messages []json.RawMessage
			if json.Unmarshal(raw, &wire) != nil || json.Unmarshal(wire["messages"], &messages) != nil {
				http.Error(w, "bad scripted interjection", 400)
				return
			}
			kept := make([]json.RawMessage, 0, len(messages))
			for _, message := range messages {
				var msg stubMessage
				if json.Unmarshal(message, &msg) != nil {
					http.Error(w, "bad scripted message", 400)
					return
				}
				if msg.Role == "user" && strings.HasPrefix(msg.Content, "<owner_interjected>") {
					continue
				}
				kept = append(kept, message)
			}
			wire["messages"], _ = json.Marshal(kept)
			raw, _ = json.Marshal(wire)
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		m.base.handle(w, r)
	}))
	t.Cleanup(m.server.Close)
	return m
}
