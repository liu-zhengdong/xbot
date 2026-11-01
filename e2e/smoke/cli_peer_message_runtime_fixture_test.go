//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Exact independent0056 wire view, never an alias of product response types.
type cliPeerMessageReply struct {
	Bot, Route    string
	Posted        bool
	EventID       string `json:"event_id"`
	EnvelopeID    string `json:"envelope_id"`
	Gateway       string
	GatewayStatus int `json:"gateway_status"`
	Queued        bool
	Stage         string
	Recorded      *bool
	RecordSeq     int `json:"record_seq"`
	Error         string
}

type cliPeerMessageModel struct {
	base   *mcpRuntimeModel
	server *httptest.Server
	mu     sync.Mutex
	raw    [][]byte
}

func newCLIPeerMessageModel(t *testing.T) *cliPeerMessageModel {
	t.Helper()
	m := &cliPeerMessageModel{base: &mcpRuntimeModel{scripts: map[string]*mcpRuntimeScript{}}}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body bytes.Buffer
		if _, err := body.ReadFrom(r.Body); err != nil {
			http.Error(w, "cannot read scripted request", http.StatusBadRequest)
			return
		}
		r.Body.Close()
		raw := bytes.Clone(body.Bytes())
		m.mu.Lock()
		m.raw = append(m.raw, raw)
		m.mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(raw))
		m.base.handle(w, r)
	}))
	t.Cleanup(m.server.Close)
	return m
}

func (m *cliPeerMessageModel) requests(cue string) []stubRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []stubRequest
	for _, raw := range m.raw {
		var req stubRequest
		if json.Unmarshal(raw, &req) == nil && bytes.Contains([]byte(lastUser(req)), []byte(cue)) {
			out = append(out, req)
		}
	}
	return out
}
