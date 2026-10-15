package inbound

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/link"
)

func TestWebhookAuthDedupAndBurst(t *testing.T) {
	root := t.TempDir()
	b := testBot(t, root, "ada")
	hook, err := AddHook(b, "github", AuthHMAC, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(hookPath(b, "github"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0600 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
	if !b.Grants("webhook") || len(b.Config().Webhooks) != 1 {
		t.Fatalf("grant %+v", b.Config())
	}
	start := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	clock := start
	s := &Server{Root: root, Window: time.Second, Now: func() time.Time { return clock }}
	body := []byte(`{"ref":"main"}`)

	rec := post(s, "/webhook/ada/github", body, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned %d %s", rec.Code, rec.Body)
	}
	rec = post(s, "/webhook/ada/github", body, map[string]string{
		"Authorization": "Bearer " + hook.Secret,
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bearer on hmac hook %d", rec.Code)
	}
	rec = post(s, "/webhook/ada/github", body, map[string]string{
		"X-XBot-Signature": SignHMAC(hook.Secret, []byte("other")),
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad sig %d", rec.Code)
	}
	rec = post(s, "/webhook/ada/github", bytes.Repeat([]byte("x"), MaxBody+1), map[string]string{
		"X-XBot-Signature": "sha256=" + strings.Repeat("ab", 32),
	})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize %d", rec.Code)
	}

	rec = post(s, "/webhook/ada/github", body, map[string]string{
		"X-XBot-Signature": SignHMAC(hook.Secret, body),
		"X-XBot-Delivery":  "deliv1",
		"X-XBot-Event":     "push",
	})
	if rec.Code != http.StatusAccepted || statusOf(t, rec) != "accepted" {
		t.Fatalf("signed %d %s", rec.Code, rec.Body)
	}
	rec = post(s, "/webhook/ada/github", body, map[string]string{
		"X-XBot-Signature": SignHMAC(hook.Secret, body),
		"X-XBot-Delivery":  "deliv1",
	})
	if rec.Code != http.StatusOK || statusOf(t, rec) != "duplicate" {
		t.Fatalf("dup %d %s", rec.Code, rec.Body)
	}
	if err := s.Flush(start); err != nil {
		t.Fatal(err)
	}
	if n := posts(t, b.InboxPath()); n != 0 {
		t.Fatalf("flushed inside the window: %d", n)
	}
	if err := s.Flush(start.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	ev := onlyEvent(t, b)
	if ev.Source != "webhook:github" || ev.ID != "wh_github_deliv1" || !ev.Untrusted {
		t.Fatalf("event %+v", ev)
	}
	if !strings.Contains(ev.Content, `count="1"`) || !strings.Contains(ev.Content, "deliv1") {
		t.Fatalf("content %s", ev.Content)
	}
	again := &Server{Root: root, Window: time.Second, Now: func() time.Time { return clock }}
	rec = post(again, "/webhook/ada/github", body, map[string]string{
		"X-XBot-Signature": SignHMAC(hook.Secret, body),
		"X-XBot-Delivery":  "deliv1",
	})
	if rec.Code != http.StatusOK || statusOf(t, rec) != "duplicate" {
		t.Fatalf("restart dup %d %s", rec.Code, rec.Body)
	}

	for i := 0; i < 5; i++ {
		id := "burst" + string(rune('a'+i))
		rec = post(s, "/webhook/ada/github", body, map[string]string{
			"X-XBot-Signature": SignHMAC(hook.Secret, body),
			"X-XBot-Delivery":  id,
		})
		if rec.Code != http.StatusAccepted {
			t.Fatalf("burst %s %d", id, rec.Code)
		}
	}
	if err := s.Flush(start.Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	burst := onlyEvent(t, b)
	if !strings.Contains(burst.Content, `count="5"`) || !strings.Contains(burst.Content, "bursta") || !strings.Contains(burst.Content, "burste") {
		t.Fatalf("burst content %s", burst.Content)
	}
}

func TestBearerWebhook(t *testing.T) {
	root := t.TempDir()
	b := testBot(t, root, "ada")
	hook, err := AddHook(b, "phone", AuthBearer, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	s := &Server{Root: root, Window: time.Second, Now: func() time.Time { return start }}
	body := []byte("ping")
	rec := post(s, "/webhook/ada/phone", body, map[string]string{
		"X-XBot-Signature": SignHMAC(hook.Secret, body),
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("hmac on bearer hook %d", rec.Code)
	}
	rec = post(s, "/webhook/ada/phone", body, map[string]string{
		"Authorization": "Bearer " + hook.Secret[:8],
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("short token %d", rec.Code)
	}
	rec = post(s, "/webhook/ada/phone", body, map[string]string{
		"Authorization": "Bearer " + hook.Secret,
		"X-XBot-Event":  "ping",
		"X-XBot-Sender": "phone:me",
		"X-Delivery-Id": "p1",
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("bearer %d %s", rec.Code, rec.Body)
	}
	if err := s.Flush(start.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	ev := onlyEvent(t, b)
	if ev.Sender != "phone:me" || ev.Type != "ping" || ev.ID != "wh_phone_p1" {
		t.Fatalf("%+v", ev)
	}
}

func TestWatchDebounceCapsAndTraversal(t *testing.T) {
	if _, err := AddWatch(testBot(t, t.TempDir(), "ada"), "notes", "../etc/passwd"); err == nil {
		t.Fatal("accepted relative traversal")
	}
	if _, err := AddWatch(testBot(t, t.TempDir(), "ada"), "notes", "/tmp/../etc/*"); err == nil {
		t.Fatal("accepted dotdot")
	}
	if _, err := AddWatch(testBot(t, t.TempDir(), "ada"), "notes", "notes/*"); err == nil {
		t.Fatal("accepted relative")
	}
	if _, err := AddWatch(testBot(t, t.TempDir(), "ada"), "notes", "/*"); err == nil {
		t.Fatal("accepted root glob")
	}

	root := t.TempDir()
	b := testBot(t, root, "ada")
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	secret := "TOPSECRET_BODY"
	if err := os.WriteFile(path, []byte("aa"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(dir, "leak.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := AddWatch(b, "notes", filepath.Join(dir, "*.txt")); err != nil {
		t.Fatal(err)
	}
	debounce := 40 * time.Millisecond
	t0 := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	if err := DeliverFeeds(b, t0, debounce); err != nil {
		t.Fatal(err)
	}
	if n := posts(t, b.InboxPath()); n != 0 {
		t.Fatalf("baseline emitted %d", n)
	}
	if err := os.WriteFile(path, []byte("bbbb"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := DeliverFeeds(b, t0.Add(10*time.Millisecond), debounce); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("cccccc"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := DeliverFeeds(b, t0.Add(20*time.Millisecond), debounce); err != nil {
		t.Fatal(err)
	}
	if n := posts(t, b.InboxPath()); n != 0 {
		t.Fatalf("emitted inside debounce: %d", n)
	}
	if err := DeliverFeeds(b, t0.Add(20*time.Millisecond+debounce), debounce); err != nil {
		t.Fatal(err)
	}
	ev := onlyEvent(t, b)
	if ev.Source != "watch:notes" || !strings.Contains(ev.Content, path) || !strings.Contains(ev.Content, "modified") || !strings.Contains(ev.Content, "size: 6") {
		t.Fatalf("%+v", ev)
	}
	if strings.Contains(ev.Content, secret) || strings.Contains(ev.Content, "leak.txt") || strings.Contains(ev.Content, outside) {
		t.Fatalf("event leaked file data:\n%s", ev.Content)
	}
	if err := DeliverFeeds(b, t0.Add(time.Second), debounce); err != nil {
		t.Fatal(err)
	}
	if n := posts(t, b.InboxPath()); n != 1 {
		t.Fatalf("posts %d", n)
	}
}

func TestWatchHoldEmitsDuringFlood(t *testing.T) {
	root := t.TempDir()
	b := testBot(t, root, "ada")
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := AddWatch(b, "notes", filepath.Join(dir, "*.txt")); err != nil {
		t.Fatal(err)
	}
	debounce := 20 * time.Millisecond
	start := time.Date(2026, 10, 15, 11, 0, 0, 0, time.UTC)
	if err := DeliverFeeds(b, start, debounce); err != nil {
		t.Fatal(err)
	}
	body := []byte("x")
	for i := 1; i <= 21; i++ {
		body = append(body, 'x')
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
		now := start.Add(time.Duration(i) * 10 * time.Millisecond)
		if err := DeliverFeeds(b, now, debounce); err != nil {
			t.Fatal(err)
		}
	}
	if n := posts(t, b.InboxPath()); n != 1 {
		t.Fatalf("flood posts %d", n)
	}
}

func TestWatchCountCap(t *testing.T) {
	b := testBot(t, t.TempDir(), "ada")
	dir := t.TempDir()
	for i := 0; i < MaxWatches; i++ {
		id := fmt.Sprintf("w%02d", i)
		if _, err := AddWatch(b, id, filepath.Join(dir, id+".txt")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := AddWatch(b, "overflow", filepath.Join(dir, "overflow.txt")); err == nil {
		t.Fatal("expected cap")
	}
}

func TestWatchFileCap(t *testing.T) {
	b := testBot(t, t.TempDir(), "ada")
	dir := t.TempDir()
	for i := 0; i < MaxWatchFiles+5; i++ {
		path := filepath.Join(dir, fmt.Sprintf("f%03d.txt", i))
		if err := os.WriteFile(path, []byte("a"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := AddWatch(b, "many", filepath.Join(dir, "*.txt")); err != nil {
		t.Fatal(err)
	}
	if err := DeliverFeeds(b, time.Date(2026, 10, 15, 13, 0, 0, 0, time.UTC), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(statePath(b))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), `"mtime_ns"`); n != MaxWatchFiles {
		t.Fatalf("tracked %d files", n)
	}
	if posts(t, b.InboxPath()) != 0 {
		t.Fatal("baseline emitted")
	}
}

func TestNestedGlob(t *testing.T) {
	b := testBot(t, t.TempDir(), "ada")
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(dir, "a.txt")
	c := filepath.Join(dir, "sub", "c.txt")
	if err := os.WriteFile(a, []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c, []byte("c"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := AddWatch(b, "tree", filepath.Join(dir, "**", "*.txt")); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	debounce := 10 * time.Millisecond
	if err := DeliverFeeds(b, start, debounce); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a, []byte("aa"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c, []byte("cc"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := DeliverFeeds(b, start.Add(time.Millisecond), debounce); err != nil {
		t.Fatal(err)
	}
	if err := DeliverFeeds(b, start.Add(time.Millisecond+debounce), debounce); err != nil {
		t.Fatal(err)
	}
	ev := onlyEvent(t, b)
	if !strings.Contains(ev.Content, a) || !strings.Contains(ev.Content, c) || !strings.Contains(ev.Content, `count="2"`) {
		t.Fatalf("%s", ev.Content)
	}
}

func testBot(t *testing.T, root, name string) *bot.Bot {
	t.Helper()
	b, err := bot.Create(root, name, "Be brief.", []string{"terminal"})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func post(s *Server, path string, body []byte, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func statusOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var m map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	return m["status"]
}

func posts(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(raw), `"op":"post"`)
}

func onlyEvent(t *testing.T, b *bot.Bot) link.Event {
	t.Helper()
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	ev, ok := box.Head()
	if !ok {
		t.Fatal("no event")
	}
	if err := box.Ack(ev.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := box.Head(); ok {
		t.Fatal("extra event")
	}
	return ev
}
