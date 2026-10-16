package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/vault"
)

func httpTestStore(t *testing.T) *vault.Store {
	t.Helper()
	base := t.TempDir()
	s, err := vault.Open(filepath.Join(base, "vault"), filepath.Join(base, "key"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func hostOf(t *testing.T, raw string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req.URL.Hostname()
}

func TestHTTPRequestInjectsAndMasksOutput(t *testing.T) {
	const value = "s3cr3t-value-xyz"
	var seenHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenHeader = r.Header.Get("X-Token")
		_, _ = io.WriteString(w, "echo "+value)
	}))
	defer srv.Close()

	store := httpTestStore(t)
	if err := store.Set("TOKEN", value, []string{hostOf(t, srv.URL)}); err != nil {
		t.Fatal(err)
	}
	tool := &HTTPRequest{Store: store}
	args := map[string]any{
		"method":  "POST",
		"url":     srv.URL + "/x",
		"headers": map[string]string{"X-Token": "{{secret:TOKEN}}"},
		"body":    "hello",
	}
	raw, _ := json.Marshal(args)
	out, err := tool.Run(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if seenHeader != value {
		t.Fatalf("server saw %q", seenHeader)
	}
	if strings.Contains(out, value) {
		t.Fatalf("secret leaked to tool output: %q", out)
	}
	if !strings.Contains(out, "[secret]") {
		t.Fatalf("output not masked: %q", out)
	}
}

func TestHTTPRequestMasksValueAcrossTruncation(t *testing.T) {
	const value = "straddling-secret-value-123"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		padding := strings.Repeat(".", maxHTTPBody-10)
		_, _ = io.WriteString(w, padding+value+" and the body continues past the clip")
	}))
	defer srv.Close()

	store := httpTestStore(t)
	if err := store.Set("TOKEN", value, []string{hostOf(t, srv.URL)}); err != nil {
		t.Fatal(err)
	}
	tool := &HTTPRequest{Store: store}
	raw := json.RawMessage(`{"url":"` + srv.URL + `/big"}`)
	out, err := tool.Run(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[truncated]") {
		t.Fatalf("response was not truncated: %q", out[:80])
	}
	if strings.Contains(out, value) || strings.Contains(out, value[:12]) {
		t.Fatal("value survived the clip")
	}
	if !strings.Contains(out, "[secret]") {
		t.Fatal("straddling value was not masked")
	}
}

func TestHTTPRequestRefusesUnboundHost(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
	}))
	defer srv.Close()

	store := httpTestStore(t)
	if err := store.Set("TOKEN", "value", []string{"a.com"}); err != nil {
		t.Fatal(err)
	}
	tool := &HTTPRequest{Store: store}
	raw := json.RawMessage(`{"url":"` + srv.URL + `/?t={{secret:TOKEN}}"}`)
	if _, err := tool.Run(context.Background(), raw); err == nil {
		t.Fatal("request was not refused")
	}
	if hits != 0 {
		t.Fatalf("server was hit %d times", hits)
	}
}

func TestSecretListShowsNamesAndDomainsOnly(t *testing.T) {
	const value = "hidden-value"
	store := httpTestStore(t)
	if err := store.Set("TOKEN", value, []string{"a.com", "b.com"}); err != nil {
		t.Fatal(err)
	}
	tool := &SecretList{Store: store}
	out, err := tool.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "TOKEN") || !strings.Contains(out, "a.com,b.com") {
		t.Fatalf("names/domains missing: %q", out)
	}
	if strings.Contains(out, value) {
		t.Fatalf("value leaked: %q", out)
	}
}

func TestHTTPRequestConfirmDefersToKernelReview(t *testing.T) {
	tool := &HTTPRequest{}
	// The tool itself never forces a card; the kernel's central review does.
	for _, raw := range []string{
		`{"url":"https://example.com"}`,
		`{"method":"POST","url":"https://example.com","body":"x"}`,
		`{"url":"https://example.com/?k={{secret:TOKEN}}"}`,
	} {
		need, summary, err := tool.Confirm(json.RawMessage(raw))
		if err != nil || need {
			t.Fatalf("%s need=%v err=%v", raw, need, err)
		}
		if !strings.Contains(summary, "https://example.com") {
			t.Fatalf("%s summary %q", raw, summary)
		}
	}
}

func TestSecretListDoesNotAsk(t *testing.T) {
	need, _, err := (&SecretList{}).Confirm(nil)
	if err != nil || need {
		t.Fatalf("list_secrets need=%v err=%v", need, err)
	}
}
