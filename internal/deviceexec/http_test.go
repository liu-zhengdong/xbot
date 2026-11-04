package deviceexec

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func request(m *Manager, s *Source, method, path string, body []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	if s != nil {
		r = r.WithContext(WithSource(r.Context(), *s))
	}
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, r)
	return w
}

func TestHandlerRequiresTrustedIdentityAndUsesTaskDTO(t *testing.T) {
	m := testManager(t)
	s := testSource()
	call := testCall("http", "write", "write_file", map[string]any{"path": "from-http", "content": "real file"})
	body, _ := json.Marshal(call)
	r := httptest.NewRequest(http.MethodPost, "/calls", bytes.NewReader(body))
	r.Header.Set("X-Device-Source", `{"account":"account","owner":true}`)
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("untrusted headers accepted: %d", w.Code)
	}
	w = request(m, &s, http.MethodPost, "/calls", body)
	if w.Code != http.StatusAccepted {
		t.Fatalf("submit: %d %s", w.Code, w.Body.String())
	}
	var receipt struct {
		Task Task `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil || receipt.Task.ID == "" || receipt.Task.Ref != "http" {
		t.Fatalf("receipt: %s %v", w.Body.String(), err)
	}
	finished := waitDetail(t, m, s, "http")
	w = request(m, &s, http.MethodGet, "/tasks/http?limit=5", nil)
	var detail Detail
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || detail.Task.Status != "completed" || len(detail.Output) > 5 || detail.Task.DeviceID != "target" {
		t.Fatalf("detail: %d %s", w.Code, w.Body.String())
	}
	if len(finished.Task.Artifacts) != 1 {
		t.Fatalf("missing file artifact: %+v", finished)
	}
	w = request(m, &s, http.MethodGet, "/tasks/http/artifacts/a1?offset=2&limit=4", nil)
	if w.Code != http.StatusOK || w.Body.String() != "al f" || w.Header().Get("X-Next-Offset") != "6" {
		t.Fatalf("artifact: %d %q %v", w.Code, w.Body.String(), w.Header())
	}
	w = request(m, &s, http.MethodPost, "/tasks/http/stop", nil)
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil || receipt.Task.Status != "completed" {
		t.Fatalf("completed stop: %s %v", w.Body.String(), err)
	}
	other := s
	other.ContextID = "other"
	if w = request(m, &other, http.MethodGet, "/tasks/http", nil); w.Code != http.StatusNotFound {
		t.Fatalf("foreign context: %d", w.Code)
	}
	owner := Source{Account: s.Account, Owner: true}
	if w = request(m, &owner, http.MethodGet, "/tasks/http", nil); w.Code != http.StatusOK {
		t.Fatalf("owner detail: %d %s", w.Code, w.Body.String())
	}
	if w = request(m, &owner, http.MethodPost, "/calls", body); w.Code != http.StatusBadRequest {
		t.Fatalf("owner executed: %d", w.Code)
	}
	// JSON cannot bind identity, including Owner, into the handler context.
	forged := []byte(`{"ref":"forged","kind":"read","tool":"read_file","args":{"path":"x"},"source":{"owner":true}}`)
	if w = request(m, &s, http.MethodPost, "/calls", forged); w.Code != http.StatusBadRequest {
		t.Fatalf("payload identity: %d", w.Code)
	}
}

func TestHandlerRangesAndBoundedArtifactTransfer(t *testing.T) {
	m := testManager(t)
	s := testSource()
	path := filepath.Join(m.Root, "binary")
	if err := os.WriteFile(path, bytes.Repeat([]byte{0x00, 0xff, 0x42}, MaxArtifactChunk), 0600); err != nil {
		t.Fatal(err)
	}
	c := testCall("binary", "write", "write_file", map[string]any{"path": "ok", "content": "ok"})
	c.Artifacts = []string{path}
	submit(t, m, s, c)
	waitDetail(t, m, s, c.Ref)
	w := request(m, &s, http.MethodGet, "/tasks/binary/artifacts/a1?limit="+strconv.Itoa(MaxArtifactChunk*2), nil)
	if w.Code != http.StatusOK || w.Body.Len() != MaxArtifactChunk {
		t.Fatalf("chunk cap: %d %d", w.Code, w.Body.Len())
	}
	if w.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatal("binary was JSON encoded")
	}
	for _, query := range []string{"offset=-1", "offset=bad", "limit=-2", "limit=bad"} {
		w = request(m, &s, http.MethodGet, "/tasks/binary?"+query, nil)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("range %s: %d", query, w.Code)
		}
	}
	over := testCall("over", "write", "write_file", map[string]any{"path": "x", "content": strings.Repeat("x", MaxRequestBytes)})
	body, _ := json.Marshal(over)
	if w = request(m, &s, http.MethodPost, "/calls", body); w.Code != http.StatusBadRequest {
		t.Fatalf("oversized call accepted: %d", w.Code)
	}
	if len(m.List(nil)) != 1 {
		t.Fatal("invalid requests created jobs")
	}
}

func TestUnknownAcceptedIntentDoesNotReplay(t *testing.T) {
	m := testManager(t)
	s := testSource()
	c := testCall("uncertain", "write", "write_file", map[string]any{"path": "must-not-exist", "content": "bad"})
	rec := &record{Source: s, Ref: c.Ref, Fingerprint: digest(c), Kind: c.Kind, Description: c.Description}
	if err := m.save(rec); err != nil {
		t.Fatal(err)
	}
	m.Close()
	reopened, err := New(m.Root, m.DeviceID, m.DeviceName)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	task, err := reopened.Submit(context.Background(), s, c)
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "failed" || !strings.Contains(task.Error, "not replayed") {
		t.Fatalf("unknown result: %+v", task)
	}
	cdir := filepath.Join(m.Root, "deviceexec", "contexts", contextKey(s), "work", "must-not-exist")
	if _, err := os.Stat(cdir); !os.IsNotExist(err) {
		t.Fatalf("uncertain call replayed: %v", err)
	}
}
