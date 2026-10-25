package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"testing"

	"xbot/internal/config"
)

func TestWebSearchSettingsAPI(t *testing.T) {
	root := t.TempDir()
	defer config.ForgetKeys()
	s := &Server{Root: root, Owner: "owner"}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	mux := muxOf(s)
	calls := 0
	if err := config.SaveWeb(root, config.WebFile{TrustedEgressCIDRs: []string{"198.18.0.0/15"}}); err != nil {
		t.Fatal(err)
	}
	s.searchProbe = func(ctx context.Context, id, key string, trusted []netip.Prefix) error {
		if len(trusted) != 1 || trusted[0].String() != "198.18.0.0/15" {
			t.Error("probe lost trusted egress", trusted)
		}
		calls++
		if id != "deepseek" || key != "fake-valid-search-key" {
			return errors.New("fake rejected: " + key)
		}
		return nil
	}
	call := func(method, path, body string, auth bool) (int, string) {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if auth {
			r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	for _, p := range []struct{ method, path string }{{"GET", "/api/web-search"}, {"PUT", "/api/web-search"}, {"POST", "/api/web-search/key"}} {
		if code, _ := call(p.method, p.path, `{}`, false); code != 401 {
			t.Fatal(code)
		}
	}
	if code, raw := call("GET", "/api/web-search", "", true); code != 200 || !strings.Contains(raw, `"provider":"ddg"`) || !strings.Contains(raw, `"enabled":true`) {
		t.Fatalf("default %d %s", code, raw)
	}

	for _, body := range []string{`{}`, `{"enabled":true,"mode":"bad","provider":"ddg"}`, `{"enabled":true,"mode":"auto","provider":"bocha"}`, `{"enabled":true,"mode":"auto","provider":"ddg","key":"no"}`, `{} {}`, strings.Repeat("x", 4097)} {
		if code, _ := call("PUT", "/api/web-search", body, true); code != 400 {
			t.Fatal(code)
		}
	}
	if code, raw := call("PUT", "/api/web-search", `{"enabled":false,"mode":"local","provider":"deepseek"}`, true); code != 200 || !strings.Contains(raw, `"enabled":false`) {
		t.Fatalf("save %d %s", code, raw)
	}
	for _, key := range []string{"", "short", "fake-invalid-search-key"} {
		if code, raw := call("POST", "/api/web-search/key", `{"provider":"deepseek","key":"`+key+`"}`, true); code < 400 || strings.Contains(raw, key) && len(key) > 0 {
			t.Fatalf("rejection/leak %d %s", code, raw)
		}
	}
	if _, err := os.Stat(config.EnvFile(root)); !os.IsNotExist(err) {
		t.Fatal("failed key persisted")
	}
	if code, raw := call("POST", "/api/web-search/key", `{"provider":"deepseek","key":"fake-valid-search-key"}`, true); code != 200 || strings.Contains(raw, "fake-valid") {
		t.Fatalf("probe %d %s", code, raw)
	}
	if code, raw := call("GET", "/api/web-search", "", true); code != 200 || !strings.Contains(raw, `"key_configured":true`) || strings.Contains(raw, "fake-valid") {
		t.Fatalf("readback %d %s", code, raw)
	}
	if code, _ := call("POST", "/api/web-search/key", `{"provider":"deepseek","key":""}`, true); code != 200 {
		t.Fatal("stored credential cannot be retried")
	}
	if calls != 3 {
		t.Fatal(calls)
	}
	r := httptest.NewRequest("PUT", "/api/web-search", strings.NewReader(`{"enabled":true,"mode":"auto","provider":"ddg"}`))
	r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	r.Header.Set("Origin", "https://other.example")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	f, _ := config.LoadWeb(root)
	if f.SearchOn() {
		t.Fatal("cross-origin mutation")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r = httptest.NewRequest("POST", "/api/web-search/key", strings.NewReader(`{"provider":"deepseek","key":"fake-valid-search-key"}`)).WithContext(ctx)
	r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 422 {
		t.Fatal(w.Code)
	}
}
