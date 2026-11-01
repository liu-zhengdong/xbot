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

type cliMemoryRecallReply struct {
	Text, File, Access string
	Bytes              int64
	Error              string
}

// The provider alone is scripted. The original Shell creates its actual live
// CallID/prepared source; recall/person selection and file reads are real.
type cliMemoryRecallModel struct {
	base   *mcpRuntimeModel
	server *httptest.Server
	mu     sync.Mutex
	raw    [][]byte
}

func newCLIMemoryRecallModel(t *testing.T) *cliMemoryRecallModel {
	t.Helper()
	m := &cliMemoryRecallModel{base: &mcpRuntimeModel{scripts: map[string]*mcpRuntimeScript{}}}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad scripted recall request", 400)
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
