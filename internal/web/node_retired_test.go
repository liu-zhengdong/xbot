package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// The old node feature is gone: its routes are gone and its files are ignored.

func TestNodePendingRoutesGone(t *testing.T) {
	s, tok, _ := statusFixture(t, 1)
	ui := httptest.NewServer(muxOf(s))
	defer ui.Close()
	cookie := loginCookie(t, ui.URL, tok)

	// Every method on a retired route must miss; none may answer 200.
	cases := []struct{ method, path, body string }{
		{http.MethodGet, "/api/node/pending", ""},
		{http.MethodGet, "/api/node/pending/abc", ""},
		{http.MethodPost, "/api/node/pending/x", `{}`},
		{http.MethodPost, "/api/node/pending", `{"id":"abc"}`},
	}
	for _, c := range cases {
		res := sendJSON(t, c.method, ui.URL+c.path, cookie, c.body)
		if res.StatusCode == http.StatusOK {
			t.Fatalf("%s %s still answered 200", c.method, c.path)
		}
		body := string(res.Body)
		for _, key := range []string{`"items"`, `"command"`, `"id":"abc"`} {
			if strings.Contains(body, key) {
				t.Fatalf("%s %s: body lists %s: %s", c.method, c.path, key, body)
			}
		}
	}
}

// legacyNodeFile records bytes and mtime so a later request can prove it was not touched.
type legacyNodeFile struct {
	path string
	raw  []byte
	mod  int64
}

func captureLegacyFile(t *testing.T, path string) legacyNodeFile {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return legacyNodeFile{path: path, raw: raw, mod: fi.ModTime().UnixNano()}
}

func (f legacyNodeFile) unchanged(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatalf("%s gone: %v", f.path, err)
	}
	if string(raw) != string(f.raw) {
		t.Fatalf("%s changed: %q", f.path, raw)
	}
	fi, err := os.Stat(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.ModTime().UnixNano() != f.mod {
		t.Fatalf("%s mtime changed", f.path)
	}
}

func TestLegacyNodeStateIgnored(t *testing.T) {
	var hits atomic.Int64
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"retired-node-zq","name":"retired-node-zq","bots":[]}`))
	}))
	defer fake.Close()

	s, tok, bots := statusFixture(t, 2)
	// Same shape as the retired registry: {"nodes":[{name,url,token}]}.
	write := func(rel, body string) string {
		p := filepath.Join(s.Root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	files := []legacyNodeFile{
		captureLegacyFile(t, write("nodes.json", `{"nodes":[{"name":"retired-node-zq","url":"`+fake.URL+`","token":"x"}]}`)),
		captureLegacyFile(t, write("node-token", "x")),
		captureLegacyFile(t, write(filepath.Join("node-pending", "abc.json"), `{"id":"abc","command":"echo hi"}`)),
	}

	ui := httptest.NewServer(muxOf(s))
	defer ui.Close()
	cookie := loginCookie(t, ui.URL, tok)

	// JSON endpoints the status UI and sidebar use.
	paths := []string{
		"/api/bots/status",
		"/api/bots",
		"/api/groups",
		"/api/bots/" + bots[0].Name(),
		"/api/notes",
	}
	for i := 0; i < 3; i++ {
		for _, p := range paths {
			body := string(getAuth(t, ui.URL+p, cookie))
			if strings.Contains(body, "retired-node-zq") || strings.Contains(body, fake.URL) {
				t.Fatalf("%s leaked legacy node state: %s", p, body)
			}
		}
		if body := string(getAuth(t, ui.URL+"/api/session", cookie)); strings.Contains(body, fake.URL) {
			t.Fatalf("/api/session leaked %s", fake.URL)
		}
	}

	if n := hits.Load(); n != 0 {
		t.Fatalf("legacy nodes.json caused %d outbound calls to %s", n, fake.URL)
	}
	for _, f := range files {
		f.unchanged(t)
	}
}
