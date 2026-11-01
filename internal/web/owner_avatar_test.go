package web

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestOwnerAvatarSeam(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "owner"}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	mux := muxOf(s)
	call := func(method, path string, data []byte, auth bool, origin string) *httptest.ResponseRecorder {
		t.Helper()
		var body io.Reader
		ct := ""
		if data != nil {
			var b bytes.Buffer
			m := multipart.NewWriter(&b)
			f, _ := m.CreateFormFile("file", "face.png")
			_, _ = f.Write(data)
			_ = m.Close()
			body = &b
			ct = m.FormDataContentType()
		}
		r := httptest.NewRequest(method, path, body)
		if auth {
			r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		}
		if ct != "" {
			r.Header.Set("Content-Type", ct)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	sessionURL := func() string {
		t.Helper()
		w := call("GET", "/api/session", nil, true, "")
		var v map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		u, ok := v["owner_avatar_url"].(string)
		if !ok {
			t.Fatal("session missing owner_avatar_url")
		}
		return u
	}
	if sessionURL() != "" {
		t.Fatal("fresh root has custom avatar")
	}
	for _, method := range []string{"GET", "POST", "DELETE"} {
		if w := call(method, "/api/owner/avatar", nil, false, ""); w.Code != 401 {
			t.Fatalf("unauth %s: %d", method, w.Code)
		}
	}
	png := []byte("\x89PNG\r\n\x1a\nfixture")
	if w := call("POST", "/api/owner/avatar", png, true, "https://elsewhere.invalid"); w.Code != 403 {
		t.Fatalf("cross origin: %d", w.Code)
	}
	if w := call("POST", "/api/owner/avatar", png, true, ""); w.Code != 200 {
		t.Fatalf("upload: %d %s", w.Code, w.Body)
	}
	if sessionURL() != "/api/owner/avatar" {
		t.Fatal("session did not expose upload")
	}
	// A new Server uses the same disk truth, not process state.
	next := &Server{Root: root}
	if next.ownerAvatarURL() != "/api/owner/avatar" {
		t.Fatal("upload not persistent")
	}
	raw, err := os.ReadFile(filepath.Join(root, "owner-avatar"))
	if err != nil || !bytes.Equal(raw, png) {
		t.Fatal("disk mismatch", err)
	}
	w := call("GET", "/api/owner/avatar", nil, true, "")
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), png) || w.Header().Get("Content-Type") != "image/png" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Cache-Control") != "private, no-cache" {
		t.Fatalf("read: %d %v", w.Code, w.Header())
	}
	for _, bad := range []struct {
		data []byte
		code int
	}{{[]byte("<svg>"), 400}, {append(png, make([]byte, 1<<20)...), 413}} {
		if w := call("POST", "/api/owner/avatar", bad.data, true, ""); w.Code != bad.code {
			t.Fatalf("invalid upload: %d want %d", w.Code, bad.code)
		}
		if w := call("GET", "/api/owner/avatar", nil, true, ""); !bytes.Equal(w.Body.Bytes(), png) {
			t.Fatal("failed upload changed picture")
		}
	}
	for i := 0; i < 2; i++ {
		if w := call("DELETE", "/api/owner/avatar", nil, true, ""); w.Code != 200 {
			t.Fatalf("clear: %d", w.Code)
		}
	}
	if sessionURL() != "" {
		t.Fatal("clear left custom URL")
	}
	if w := call("GET", "/api/owner/avatar", nil, true, ""); w.Code != 404 {
		t.Fatalf("clear read: %d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(root, "owner-avatar")); !os.IsNotExist(err) {
		t.Fatal("clear left disk file")
	}
}
