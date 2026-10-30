package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/provider"
)

func TestTranscriptConditional(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	token, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	appendRow := func() {
		t.Helper()
		if err := b.Append(bot.Entry{Time: time.Now(), Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("hello")}}); err != nil {
			t.Fatal(err)
		}
	}
	appendRow()
	handler := muxOf(s)
	get := func(tag string, auth bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", "/api/bots/ada/transcript", nil)
		if auth {
			r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
		}
		r.Header.Set("If-None-Match", tag)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	first := get("", true)
	tag := first.Header().Get("ETag")
	if first.Code != 200 || tag == "" {
		t.Fatalf("first: %d etag=%q", first.Code, tag)
	}
	same := get(tag, true)
	if same.Code != 304 || same.Body.Len() != 0 {
		t.Fatalf("unchanged: %d bytes=%d", same.Code, same.Body.Len())
	}
	latencies := make([]time.Duration, 100)
	for i := range latencies {
		start := time.Now()
		if get(tag, true).Code != 304 {
			t.Fatal("idle poll changed")
		}
		latencies[i] = time.Since(start)
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	t.Logf("idle conditional handler p95=%s, body=0 bytes (100 requests)", latencies[94])
	if get(tag, false).Code != 401 {
		t.Fatal("conditional bypassed auth")
	}
	appendRow()
	changed := get(tag, true)
	if changed.Code != 200 || changed.Header().Get("ETag") == tag {
		t.Fatal("append did not invalidate ETag")
	}
	tag = changed.Header().Get("ETag")
	dir := filepath.Join(root, "remote")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "devices.json"), []byte(`{"devices":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if get(tag, true).Code != 200 {
		t.Fatal("device projection change did not invalidate ETag")
	}
	// A 304 must not decode the transcript at all. Keep tail metadata intact,
	// but make history unreadable: an unconditional read would now fail.
	clean := get("", true)
	tag = clean.Header().Get("ETag")
	segs, err := filepath.Glob(filepath.Join(bot.Dir(root, "ada"), "transcript", "*.jsonl"))
	if err != nil || len(segs) == 0 {
		t.Fatalf("segments: %v %v", segs, err)
	}
	fi, err := os.Stat(segs[len(segs)-1])
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(segs[len(segs)-1])
	if err != nil {
		t.Fatal(err)
	}
	for i := range raw {
		raw[i] = 'x'
	}
	if err := os.WriteFile(segs[len(segs)-1], raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(segs[len(segs)-1], fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}
	if get(tag, true).Code != 304 {
		t.Fatal("conditional read touched history")
	}
}
