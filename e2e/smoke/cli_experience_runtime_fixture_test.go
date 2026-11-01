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
		r.Body = io.NopCloser(bytes.NewReader(raw))
		m.base.handle(w, r)
	}))
	t.Cleanup(m.server.Close)
	return m
}
