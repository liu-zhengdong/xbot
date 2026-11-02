//go:build smoke

package smoke

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type cliTranscriptReadReply struct {
	Text, File, Access string
	Bytes              int64
	Error              string
}

// Only the provider is scripted. Source methods, FTS, original paired tool
// results, blob hydration, owner masking and semantic output remain real.
type cliTranscriptModel struct {
	base   *mcpRuntimeModel
	server *httptest.Server
	mu     sync.Mutex
	raw    [][]byte
}

func newCLITranscriptModel(t *testing.T) *cliTranscriptModel {
	t.Helper()
	m := &cliTranscriptModel{base: &mcpRuntimeModel{scripts: map[string]*mcpRuntimeScript{}}}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad scripted transcript request", 400)
			return
		}
		r.Body.Close()
		m.mu.Lock()
		m.raw = append(m.raw, bytes.Clone(raw))
		m.mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(raw))
		m.base.handle(w, r)
	}))
	t.Cleanup(m.server.Close)
	return m
}
