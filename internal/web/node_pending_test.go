package web

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/node"
)

func TestNodePendingAPI(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bot.Create(root, "ada", "Be brief.", nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "machines.json"), []byte(`{"machines":{"local":"ask"}}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "ran")
	_, err = node.RunShell(root, "touch "+marker, "10.2.3.4:5", true)
	if err == nil || !strings.Contains(err.Error(), "approve") {
		t.Fatal(err)
	}
	srv := httptest.NewServer(muxOf(s))
	defer srv.Close()
	body := getPending(t, srv, tok)
	if !strings.Contains(body, "touch "+marker) || !strings.Contains(body, "10.2.3.4:5") {
		t.Fatalf("%s", body)
	}
	var listed struct {
		Pending []node.Pending `json:"pending"`
	}
	if err := json.Unmarshal([]byte(body), &listed); err != nil || len(listed.Pending) != 1 {
		t.Fatalf("%s %v", body, err)
	}
	res := postPending(t, srv, tok, listed.Pending[0].ID, `{"allow":false}`)
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", res.StatusCode, raw)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("dismiss ran the command")
	}
	if body = getPending(t, srv, tok); strings.Contains(body, listed.Pending[0].ID) {
		t.Fatalf("still listed %s", body)
	}
	js, err := staticFS.ReadFile("static/app.js")
	if err != nil || !bytes.Contains(js, []byte("/api/node/pending")) {
		t.Fatal("web ui does not show node pending")
	}
}

func getPending(t *testing.T, srv *httptest.Server, tok string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/node/pending", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", res.StatusCode, raw)
	}
	return string(raw)
}

func postPending(t *testing.T, srv *httptest.Server, tok, id, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/node/pending/"+id, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}
