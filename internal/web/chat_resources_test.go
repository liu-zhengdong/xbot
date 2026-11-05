package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func resourceTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	s := &Server{Root: t.TempDir(), Owner: "u1"}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	return s, token
}

func resourceTestCall(t *testing.T, s *Server, token, method, target, body, origin string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/resources", s.resource)
	mux.HandleFunc("POST /api/resources/open", s.openResource)
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	if token != "" {
		r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func resourceTestJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if w.Body.Len() >= 1<<20 {
		t.Fatalf("resource response exceeds budget: %d bytes", w.Body.Len())
	}
	if w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content type: %q", w.Header().Get("Content-Type"))
	}
	var page map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	return page
}

func TestResourceAuthenticationAndInput(t *testing.T) {
	s, token := resourceTestServer(t)
	dir := t.TempDir()
	target := "/api/resources?path=" + url.QueryEscape(dir)
	for _, method := range []string{"GET", "POST"} {
		endpoint, body := target, ""
		if method == "POST" {
			endpoint, body = "/api/resources/open", fmt.Sprintf(`{"path":%q}`, dir)
		}
		w := resourceTestCall(t, s, "", method, endpoint, body, "")
		if w.Code != http.StatusUnauthorized || resourceTestJSON(t, w)["error"] != "unauthorized" {
			t.Fatalf("unauthenticated %s: %d", method, w.Code)
		}
	}
	for _, path := range []string{"", "relative.txt", "../outside", "~", "~other/file", dir + "\x00bad", strings.Repeat("a", 4097)} {
		w := resourceTestCall(t, s, token, "GET", "/api/resources?path="+url.QueryEscape(path), "", "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("path %q: %d", path, w.Code)
		}
		body, err := json.Marshal(map[string]string{"path": path})
		if err != nil {
			t.Fatal(err)
		}
		w = resourceTestCall(t, s, token, "POST", "/api/resources/open", string(body), "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("POST path %q: %d", path, w.Code)
		}
	}
	foreign := `C:\Users\owner\file.txt`
	if runtime.GOOS == "windows" {
		foreign = "/Users/owner/file.txt"
	}
	w := resourceTestCall(t, s, token, "GET", "/api/resources?path="+url.QueryEscape(foreign), "", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("foreign platform path: %d", w.Code)
	}
	for _, query := range []string{"offset=-1", "offset=no", "line=0", "line=-1", "line=no", "line=9999999999999999999999999"} {
		w := resourceTestCall(t, s, token, "GET", target+"&"+query, "", "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("query %s: %d", query, w.Code)
		}
	}
	for _, body := range []string{`{`, `{"path":"/tmp","extra":true}`} {
		w := resourceTestCall(t, s, token, "POST", "/api/resources/open", body, "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("bad body: %d", w.Code)
		}
	}
}

func TestResourceDirectoryPaginationAndHome(t *testing.T) {
	s, token := resourceTestServer(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, "中文 目录")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	want := make(map[string]bool)
	for i := 0; i < 83; i++ {
		name := fmt.Sprintf("条目 %03d.txt", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fixture\n"), 0600); err != nil {
			t.Fatal(err)
		}
		want[name] = true
	}
	seen := make(map[string]bool)
	offset := 0
	for pages := 0; ; pages++ {
		if pages > 3 {
			t.Fatal("directory pagination did not finish")
		}
		path := dir
		if pages == 0 {
			path = "~/中文 目录"
		}
		w := resourceTestCall(t, s, token, "GET", "/api/resources?path="+url.QueryEscape(path)+"&offset="+strconv.Itoa(offset), "", "")
		page := resourceTestJSON(t, w)
		host, err := os.Hostname()
		if err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || page["path"] != dir || page["name"] != filepath.Base(dir) || page["kind"] != "directory" || page["device"] != host || page["platform"] != runtime.GOOS || page["can_open"] != false {
			t.Fatalf("directory metadata: %d %+v", w.Code, page)
		}
		entries := page["entries"].([]any)
		if len(entries) > 40 {
			t.Fatalf("directory page size: %d", len(entries))
		}
		for _, raw := range entries {
			entry := raw.(map[string]any)
			name := entry["name"].(string)
			if len(entry) != 2 || entry["kind"] != "file" || !want[name] || seen[name] {
				t.Fatalf("directory entry: %+v", entry)
			}
			seen[name] = true
		}
		next, ok := page["next_offset"].(float64)
		if !ok {
			break
		}
		if int(next) != offset+len(entries) {
			t.Fatalf("next offset: %v", next)
		}
		offset = int(next)
	}
	if len(seen) != len(want) {
		t.Fatalf("entries: %d, want %d", len(seen), len(want))
	}
	empty := t.TempDir()
	w := resourceTestCall(t, s, token, "GET", "/api/resources?path="+url.QueryEscape(empty), "", "")
	if entries, ok := resourceTestJSON(t, w)["entries"].([]any); !ok || len(entries) != 0 {
		t.Fatal("empty directory must return entries: []")
	}
}

func TestResourceTextLinePagination(t *testing.T) {
	s, token := resourceTestServer(t)
	path := filepath.Join(t.TempDir(), "中文 空格.key")
	var lines []string
	for i := 1; i <= 230; i++ {
		lines = append(lines, fmt.Sprintf("第 %03d 行 %s\r\n", i, strings.Repeat("<>&中文", 90)))
	}
	lines[len(lines)-1] = strings.TrimSuffix(lines[len(lines)-1], "\r\n")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "")), 0600); err != nil {
		t.Fatal(err)
	}
	var combined strings.Builder
	line := 112
	for pages := 0; ; pages++ {
		if pages > 5 {
			t.Fatal("text pagination did not finish")
		}
		w := resourceTestCall(t, s, token, "GET", "/api/resources?path="+url.QueryEscape(path)+"&line="+strconv.Itoa(line), "", "")
		page := resourceTestJSON(t, w)
		text, ok := page["text"].(string)
		if w.Code != 200 || page["kind"] != "file" || page["start_line"] != float64(line) || !ok || len(text) > resourceTextLimit || !strings.HasPrefix(text, lines[line-1]) {
			t.Fatalf("line page: %d start=%v bytes=%d error=%v", w.Code, page["start_line"], len(text), page["preview_error"])
		}
		if strings.Contains(w.Body.String(), "<>&") {
			t.Fatal("HTML characters must remain escaped in JSON")
		}
		combined.WriteString(text)
		next, ok := page["next_line"].(float64)
		if !ok {
			break
		}
		if int(next) != line+strings.Count(text, "\n") {
			t.Fatalf("next line %v after line %d", next, line)
		}
		line = int(next)
	}
	if combined.String() != strings.Join(lines[111:], "") {
		t.Fatal("line pages lost or repeated source bytes")
	}
	for _, requested := range []int{1, 999} {
		empty := filepath.Join(t.TempDir(), "empty.txt")
		if err := os.WriteFile(empty, nil, 0600); err != nil {
			t.Fatal(err)
		}
		w := resourceTestCall(t, s, token, "GET", "/api/resources?path="+url.QueryEscape(empty)+"&line="+strconv.Itoa(requested), "", "")
		page := resourceTestJSON(t, w)
		if page["text"] != "" || page["start_line"] != float64(requested) || page["next_line"] != nil {
			t.Fatalf("empty file: %+v", page)
		}
	}
}

func TestResourcePreviewLimitsAndBinary(t *testing.T) {
	s, token := resourceTestServer(t)
	for _, tc := range []struct {
		name, content string
		previewError  bool
	}{
		{"binary.bin", "image\x00data", true},
		{"non-utf8.txt", "text\xff\n", true},
		{"long.txt", strings.Repeat("x", resourceTextLimit+1), true},
		{"exact.txt", strings.Repeat("x", resourceTextLimit), false},
		{"exact-newline.txt", strings.Repeat("x", resourceTextLimit-1) + "\nsecond\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.name)
			if err := os.WriteFile(path, []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			w := resourceTestCall(t, s, token, "GET", "/api/resources?path="+url.QueryEscape(path), "", "")
			page := resourceTestJSON(t, w)
			if w.Code != 200 || page["kind"] != "file" || page["path"] != path {
				t.Fatalf("file metadata: %d %+v", w.Code, page)
			}
			if tc.previewError {
				if page["preview_error"] == nil || page["text"] != nil || page["next_line"] != nil {
					t.Fatalf("unavailable preview: %+v", page)
				}
			} else if page["preview_error"] != nil || len(page["text"].(string)) != resourceTextLimit {
				t.Fatalf("exact budget: error=%v", page["preview_error"])
			}
			if tc.name == "exact-newline.txt" && page["next_line"] != float64(2) {
				t.Fatalf("exact first line continuation: %v", page["next_line"])
			}
		})
	}
	// A sparse binary fixture must not cause a whole-file read.
	large := filepath.Join(t.TempDir(), "large.bin")
	f, err := os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(1 << 30); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	w := resourceTestCall(t, s, token, "GET", "/api/resources?path="+url.QueryEscape(large), "", "")
	page := resourceTestJSON(t, w)
	if w.Code != 200 || page["kind"] != "file" || page["preview_error"] == nil {
		t.Fatalf("large binary: %d %+v", w.Code, page)
	}
}

func TestResourceMissingAndUnsupported(t *testing.T) {
	s, token := resourceTestServer(t)
	s.App, s.OpenPath = true, func(string) error { t.Fatal("unexpected OpenPath"); return nil }
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.txt")
	if err := os.WriteFile(plain, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, missing := range []string{filepath.Join(dir, "不存在.txt"), filepath.Join(plain, "child")} {
		for _, method := range []string{"GET", "POST"} {
			target, body := "/api/resources?path="+url.QueryEscape(missing), ""
			if method == "POST" {
				target, body = "/api/resources/open", fmt.Sprintf(`{"path":%q}`, missing)
			}
			w := resourceTestCall(t, s, token, method, target, body, "")
			if w.Code != 404 || resourceTestJSON(t, w)["error"] != "文件或目录不存在" {
				t.Fatalf("missing %s: %d", method, w.Code)
			}
		}
	}
	t.Run("socket", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "socket")
		listener, err := net.Listen("unix", path)
		if err != nil {
			t.Skipf("Unix sockets unavailable: %v", err)
		}
		defer listener.Close()
		w := resourceTestCall(t, s, token, "GET", "/api/resources?path="+url.QueryEscape(path), "", "")
		page := resourceTestJSON(t, w)
		if w.Code != 200 || page["kind"] != "unsupported" || page["can_open"] != false || page["preview_error"] == nil {
			t.Fatalf("unsupported: %d %+v", w.Code, page)
		}
		w = resourceTestCall(t, s, token, "POST", "/api/resources/open", fmt.Sprintf(`{"path":%q}`, path), "")
		if w.Code != 400 {
			t.Fatalf("open unsupported: %d", w.Code)
		}
	})
}

func TestResourceOpenPathAndSameOrigin(t *testing.T) {
	s, token := resourceTestServer(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "中文 空格.txt")
	if err := os.WriteFile(file, []byte("fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var opened []string
	s.App = true
	s.OpenPath = func(path string) error { opened = append(opened, path); return nil }
	for _, path := range []string{file, dir} {
		w := resourceTestCall(t, s, token, "GET", "/api/resources?path="+url.QueryEscape(path), "", "")
		if resourceTestJSON(t, w)["can_open"] != true || len(opened) != 0 {
			t.Fatal("GET must describe capability without calling OpenPath")
		}
	}
	body := fmt.Sprintf(`{"path":%q}`, file)
	w := resourceTestCall(t, s, token, "POST", "/api/resources/open", body, "http://evil.example")
	if w.Code != 403 || resourceTestJSON(t, w)["error"] != "cross-origin" || len(opened) != 0 {
		t.Fatal("cross-origin write must not call OpenPath")
	}
	for _, path := range []string{file, dir} {
		w := resourceTestCall(t, s, token, "POST", "/api/resources/open", fmt.Sprintf(`{"path":%q}`, path), "http://example.com")
		if w.Code != 200 || resourceTestJSON(t, w)["status"] != "ok" || opened[len(opened)-1] != path {
			t.Fatalf("same-origin open: %d %+v", w.Code, opened)
		}
	}
	if len(opened) != 2 {
		t.Fatalf("OpenPath calls: %v", opened)
	}
	s.OpenPath = func(string) error { return errors.New("fixture file manager failed") }
	w = resourceTestCall(t, s, token, "POST", "/api/resources/open", body, "")
	if w.Code != 500 || resourceTestJSON(t, w)["error"] != "fixture file manager failed" {
		t.Fatalf("callback error: %d", w.Code)
	}
	for _, callbackPresent := range []bool{false, true} {
		s.App = !callbackPresent
		if callbackPresent {
			s.OpenPath = func(string) error { t.Fatal("unexpected OpenPath without App"); return nil }
		} else {
			s.OpenPath = nil
		}
		w := resourceTestCall(t, s, token, "GET", "/api/resources?path="+url.QueryEscape(file), "", "")
		if resourceTestJSON(t, w)["can_open"] != false {
			t.Fatal("missing desktop capability must hide open action")
		}
		w = resourceTestCall(t, s, token, "POST", "/api/resources/open", body, "")
		if w.Code != 404 {
			t.Fatalf("unavailable OpenPath: %d", w.Code)
		}
	}
}

func TestResourceSymlinkTargets(t *testing.T) {
	s, token := resourceTestServer(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(file, []byte("linked content\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{file, dir} {
		path := filepath.Join(t.TempDir(), "link")
		if err := os.Symlink(target, path); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		w := resourceTestCall(t, s, token, "GET", "/api/resources?path="+url.QueryEscape(path), "", "")
		page := resourceTestJSON(t, w)
		kind := "file"
		if target == dir {
			kind = "directory"
		} else if page["text"] != "linked content\n" {
			t.Fatal("symlink must read target content")
		}
		if w.Code != 200 || page["kind"] != kind {
			t.Fatalf("symlink target: %d %+v", w.Code, page)
		}
	}
}

func TestResourceCanceledRequest(t *testing.T) {
	s, token := resourceTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("GET", "/api/resources?path="+url.QueryEscape(t.TempDir()), nil).WithContext(ctx)
	r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	w := httptest.NewRecorder()
	s.resource(w, r)
	if w.Body.Len() != 0 {
		t.Fatal("canceled resource request must stop before reading")
	}
}
