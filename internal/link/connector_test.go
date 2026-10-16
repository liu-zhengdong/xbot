package link

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSendStaysDraftUntilRun(t *testing.T) {
	const secret = "token-value-1"
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "secrets.json")
	if err := WriteSecrets(secretPath, map[string]string{secretTelegram: secret}); err != nil {
		t.Fatal(err)
	}
	var posted int
	c := NewConnector(dir, secretPath, NameTelegram)
	c.Post = func(_ context.Context, to, body, got string) error {
		posted++
		if to != "42" || body != "hello" || got != secret {
			t.Fatalf("post %s %s %s", to, body, got)
		}
		return nil
	}
	tool := c.Tools()[0]
	args := json.RawMessage(`{"to":"42","body":"hello"}`)
	need, summary, err := tool.Confirm(args)
	if err != nil || need || !strings.Contains(summary, "send telegram draft") || strings.Contains(summary, secret) {
		t.Fatalf("confirm %v %q %v", need, summary, err)
	}
	if posted != 0 {
		t.Fatal("posted before approval")
	}
	raw, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatal(err)
	}
	drafts, err := os.ReadDir(draftDir(dir))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, ent := range drafts {
		if strings.HasPrefix(ent.Name(), "d_") {
			buf, err := os.ReadFile(draftPath(dir, strings.TrimSuffix(ent.Name(), ".json")))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(buf), secret) {
				t.Fatal(string(buf))
			}
			if strings.Contains(string(buf), `"status": "draft"`) {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("no draft")
	}
	out, err := tool.Run(context.Background(), args)
	if err != nil || !strings.HasPrefix(out, "sent d_") {
		t.Fatalf("run %q %v", out, err)
	}
	if posted != 1 {
		t.Fatalf("posted %d", posted)
	}
	out, err = tool.Run(context.Background(), args)
	if err != nil || !strings.HasPrefix(out, "sent ") || posted != 1 {
		t.Fatalf("second %q posted %d %v", out, posted, err)
	}
	if strings.Contains(string(raw), "unused") {
		t.Fatal("sanity")
	}
}

func TestPostScrubsSecret(t *testing.T) {
	const secret = "hook-secret-9"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, secret) {
			t.Fatalf("path %s", r.URL.Path)
		}
		buf, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(buf), `"text":"hi"`) {
			t.Fatalf("body %s", buf)
		}
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "failed for "+secret)
	}))
	defer srv.Close()
	err := PostLark(context.Background(), srv.URL, secret, "oc_1", "hi")
	if err == nil || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "[secret]") {
		t.Fatal(err)
	}

	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer ok.Close()
	if err := PostTelegram(context.Background(), ok.URL, "1234:ABC", "9", "hi"); err != nil {
		t.Fatal(err)
	}
	if err := PostTelegram(context.Background(), ok.URL, "bad token", "9", "hi"); err == nil || strings.Contains(err.Error(), "bad token") {
		t.Fatal(err)
	}
}
