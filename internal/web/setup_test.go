package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"xbot/internal/config"
)

const setupTestKey = "sk-first-run-0123456789"

func setupServer(t *testing.T, app bool, models http.HandlerFunc) (*Server, *httptest.Server, string) {
	t.Helper()
	config.ForgetKeys()
	t.Cleanup(config.ForgetKeys)
	t.Setenv("XBOT_API_KEY", "")
	os.Unsetenv("XBOT_API_KEY")
	up := httptest.NewServer(models)
	t.Cleanup(up.Close)
	t.Setenv("XBOT_BASE_URL", up.URL+"/v1")
	s := &Server{Root: t.TempDir(), Owner: "u1", App: app}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(muxOf(s))
	t.Cleanup(srv.Close)
	return s, srv, tok
}

func setupCall(t *testing.T, srv *httptest.Server, tok, method, path, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return res.StatusCode, out
}

func TestFirstRunKeyIsTestedSavedAndHeld(t *testing.T) {
	var auth string
	s, srv, tok := setupServer(t, true, func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"data":[]}`))
	})
	if err := os.WriteFile(filepath.Join(s.Root, "env"), []byte("# mine\nXBOT_MODEL=deepseek-chat\nexport XBOT_API_KEY=\n"), 0644); err != nil {
		t.Fatal(err)
	}
	code, sess := setupCall(t, srv, tok, http.MethodGet, "/api/session", "")
	if code != 200 || sess["needs_key"] != true || !strings.HasPrefix(sess["key_host"].(string), "127.0.0.1:") {
		t.Fatalf("session before setup: %d %v", code, sess)
	}
	code, out := setupCall(t, srv, tok, http.MethodPost, "/api/setup/key", `{"key":"  `+setupTestKey+`  "}`)
	if code != 200 {
		t.Fatalf("setup: %d %v", code, out)
	}
	if auth != "Bearer "+setupTestKey {
		t.Fatalf("test request sent Authorization %q", auth)
	}
	raw, err := os.ReadFile(filepath.Join(s.Root, "env"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "# mine\nXBOT_MODEL=deepseek-chat\nXBOT_API_KEY=" + setupTestKey + "\n"; string(raw) != want {
		t.Fatalf("env file:\n%s\nwant:\n%s", raw, want)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(filepath.Join(s.Root, "env"))
		if fi.Mode().Perm() != 0600 {
			t.Fatalf("env mode %o", fi.Mode().Perm())
		}
	}
	if v, ok := config.Held("XBOT_API_KEY"); !ok || v != setupTestKey {
		t.Fatalf("key not held in memory")
	}
	if _, set := os.LookupEnv("XBOT_API_KEY"); set {
		t.Fatal("setup must not put the key in the process environment")
	}
	if code, sess = setupCall(t, srv, tok, http.MethodGet, "/api/session", ""); sess["needs_key"] != nil {
		t.Fatalf("session after setup still asks: %v", sess)
	}
	if code, _ := setupCall(t, srv, tok, http.MethodPost, "/api/setup/key", `{"key":"sk-second-0123456789"}`); code != http.StatusConflict {
		t.Fatalf("second setup: %d", code)
	}
	if v, _ := config.Held("XBOT_API_KEY"); v != setupTestKey {
		t.Fatal("second setup replaced the key")
	}
}

func TestFirstRunDoesNotFollowRedirects(t *testing.T) {
	var leaked string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("Authorization")
	}))
	defer other.Close()
	s, srv, tok := setupServer(t, true, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/models", http.StatusFound)
	})
	code, out := setupCall(t, srv, tok, http.MethodPost, "/api/setup/key", `{"key":"`+setupTestKey+`"}`)
	if code != http.StatusUnprocessableEntity || leaked != "" {
		t.Fatalf("redirect: %d %v leaked=%v", code, out, leaked != "")
	}
	if _, err := os.Stat(filepath.Join(s.Root, "env")); !os.IsNotExist(err) {
		t.Fatal("env written after a redirect")
	}
}

func TestFirstRunRejectedKeyIsNotSaved(t *testing.T) {
	s, srv, tok := setupServer(t, true, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusUnauthorized)
	})
	code, out := setupCall(t, srv, tok, http.MethodPost, "/api/setup/key", `{"key":"`+setupTestKey+`"}`)
	msg, _ := out["error"].(string)
	if code != http.StatusBadRequest || !strings.Contains(msg, "401") || strings.Contains(msg, setupTestKey) {
		t.Fatalf("rejected key: %d %q", code, msg)
	}
	if _, err := os.Stat(filepath.Join(s.Root, "env")); !os.IsNotExist(err) {
		t.Fatalf("env file written for a rejected key: %v", err)
	}
	if _, ok := config.Held("XBOT_API_KEY"); ok {
		t.Fatal("rejected key held")
	}
	if code, _ := setupCall(t, srv, tok, http.MethodPost, "/api/setup/key", `{"key":"has space in it"}`); code != http.StatusBadRequest {
		t.Fatalf("malformed key: %d", code)
	}
}

func TestFirstRunOnlyInDesktopApp(t *testing.T) {
	_, srv, tok := setupServer(t, false, func(w http.ResponseWriter, r *http.Request) {
		t.Error("serve mode must not test keys")
	})
	if _, sess := setupCall(t, srv, tok, http.MethodGet, "/api/session", ""); sess["needs_key"] != nil {
		t.Fatalf("serve session asks for a key: %v", sess)
	}
	if code, _ := setupCall(t, srv, tok, http.MethodPost, "/api/setup/key", `{"key":"`+setupTestKey+`"}`); code != http.StatusNotFound {
		t.Fatalf("serve mode setup: %d", code)
	}
}

func TestFirstRunSkippedWhenKeyExists(t *testing.T) {
	_, srv, tok := setupServer(t, true, func(w http.ResponseWriter, r *http.Request) {})
	config.SetHeld("XBOT_API_KEY", setupTestKey) // loaded from ~/.xbot/env at start
	if _, sess := setupCall(t, srv, tok, http.MethodGet, "/api/session", ""); sess["needs_key"] != nil {
		t.Fatalf("existing key still asks: %v", sess)
	}
}
