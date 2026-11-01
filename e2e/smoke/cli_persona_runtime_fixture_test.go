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

type cliPersonaReply struct {
	Name             string
	Written          bool
	Effective, Error string
}

// Observe actual provider wire, retaining only this private scripted fixture's
// requests. No Kernel, Bot snapshot, environment projection or approval is made.
type cliPersonaModel struct {
	base   *mcpRuntimeModel
	server *httptest.Server
	mu     sync.Mutex
	raw    [][]byte
}

func newCLIPersonaModel(t *testing.T) *cliPersonaModel {
	t.Helper()
	m := &cliPersonaModel{base: &mcpRuntimeModel{scripts: map[string]*mcpRuntimeScript{}}}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad scripted input", 400)
			return
		}
		m.mu.Lock()
		m.raw = append(m.raw, raw)
		m.mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(raw))
		m.base.handle(w, r)
	}))
	t.Cleanup(m.server.Close)
	return m
}
func (m *cliPersonaModel) requests(cue string) []stubRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []stubRequest
	for _, raw := range m.raw {
		var request stubRequest
		if json.Unmarshal(raw, &request) == nil && strings.Contains(lastUser(request), cue) {
			out = append(out, request)
		}
	}
	return out
}

// Independently fold only the public environment persona field in wire order;
// the complete responsibilities remain in the system prefix, not this field.
func cliPersonaEnvironment(request stubRequest) string {
	var value string
	for _, message := range request.Messages {
		rest := message.Content
		for {
			_, after, found := strings.Cut(rest, "<environment_update>\n")
			if !found {
				break
			}
			body, tail, closed := strings.Cut(after, "\n</environment_update>")
			if !closed {
				break
			}
			rest = tail
			var update struct{ Set map[string]json.RawMessage }
			if json.Unmarshal([]byte(body), &update) == nil && update.Set["persona"] != nil {
				_ = json.Unmarshal(update.Set["persona"], &value)
			}
		}
	}
	return value
}
func cliPersonaSystem(request stubRequest) string {
	var text []string
	for _, message := range request.Messages {
		if message.Role == "system" {
			text = append(text, message.Content)
		}
	}
	return strings.Join(text, "\n")
}
