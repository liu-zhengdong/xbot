package e2e

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbound"
	"xbot/internal/inbox"
	"xbot/internal/mockopenai"
	"xbot/internal/notify"
	"xbot/internal/proc"
	"xbot/internal/provider"
)

func TestSignedWebhookNotifies(t *testing.T) {
	root := t.TempDir()
	b, client := m4Bot(t, root, "ada")
	hook := m4Hook(t, b, "github", inbound.AuthHMAC)
	s, start := m4Server(root)
	body := []byte(`{"ref":"refs/heads/main"}`)
	rec := m4Post(s, "/webhook/ada/github", body, map[string]string{
		"X-XBot-Signature": inbound.SignHMAC(hook.Secret, body),
		"X-XBot-Delivery":  "deliv1",
		"X-XBot-Event":     "push",
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("signed %d %s", rec.Code, rec.Body)
	}
	unsigned := m4Post(s, "/webhook/ada/github", body, nil)
	if unsigned.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned %d", unsigned.Code)
	}
	bad := m4Post(s, "/webhook/ada/github", body, map[string]string{
		"X-XBot-Signature": inbound.SignHMAC(hook.Secret, []byte("nope")),
	})
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("bad signature %d", bad.Code)
	}
	if err := s.Flush(start.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if n := m4Posts(t, b.InboxPath()); n != 1 {
		t.Fatalf("posts %d", n)
	}
	m4Drain(t, b, client)
	notes := m4Notes(t, b)
	if len(notes) != 1 || notes[0].Text != "hook fired" {
		t.Fatalf("notes %+v", notes)
	}
	if !m4Wrapped(t, b, `source="webhook:github"`) {
		t.Fatal("webhook wake was not wrapped")
	}
}

func TestDuplicateDeliveryOnce(t *testing.T) {
	root := t.TempDir()
	b, client := m4Bot(t, root, "ada")
	hook := m4Hook(t, b, "github", inbound.AuthHMAC)
	s, start := m4Server(root)
	body := []byte(`{"n":1}`)
	hdr := map[string]string{
		"X-XBot-Signature": inbound.SignHMAC(hook.Secret, body),
		"X-XBot-Delivery":  "same",
	}
	first := m4Post(s, "/webhook/ada/github", body, hdr)
	second := m4Post(s, "/webhook/ada/github", body, hdr)
	if first.Code != http.StatusAccepted || second.Code != http.StatusOK || !strings.Contains(second.Body.String(), "duplicate") {
		t.Fatalf("first %d second %d %s", first.Code, second.Code, second.Body)
	}
	if err := s.Flush(start.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if n := m4Posts(t, b.InboxPath()); n != 1 {
		t.Fatalf("posts %d", n)
	}
	m4Drain(t, b, client)
	if notes := m4Notes(t, b); len(notes) != 1 || notes[0].Text != "hook fired" {
		t.Fatalf("notes %+v", notes)
	}
}

func TestWebhookBurstOneWake(t *testing.T) {
	root := t.TempDir()
	b, client := m4Bot(t, root, "ada")
	hook := m4Hook(t, b, "github", inbound.AuthHMAC)
	s, start := m4Server(root)
	body := []byte(`{"n":1}`)
	for i := 0; i < 5; i++ {
		id := "b" + string(rune('0'+i))
		rec := m4Post(s, "/webhook/ada/github", body, map[string]string{
			"X-XBot-Signature": inbound.SignHMAC(hook.Secret, body),
			"X-XBot-Delivery":  id,
		})
		if rec.Code != http.StatusAccepted {
			t.Fatalf("%s %d", id, rec.Code)
		}
	}
	if err := s.Flush(start.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	ev, ok := box.Head()
	if !ok || !strings.Contains(ev.Content, `count="5"`) {
		t.Fatalf("event ok=%v %s", ok, ev.Content)
	}
	if err := box.Ack(ev.ID); err != nil {
		t.Fatal(err)
	}
	if _, more := box.Head(); more {
		t.Fatal("burst produced more than one event")
	}
	k := routineKernel(b, client)
	k.Now = func() time.Time { return start }
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if notes := m4Notes(t, b); len(notes) != 1 || notes[0].Text != "hook fired" {
		t.Fatalf("notes %+v", notes)
	}
}

func TestFileWatchDebounced(t *testing.T) {
	root := t.TempDir()
	b, client := m4Bot(t, root, "ada")
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	const secret = "TOPSECRET_BODY"
	if err := os.WriteFile(path, []byte("aa"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := inbound.AddWatch(b, "notes", filepath.Join(dir, "*.txt")); err != nil {
		t.Fatal(err)
	}
	debounce := 40 * time.Millisecond
	t0 := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	if err := inbound.DeliverFeeds(b, t0, debounce); err != nil {
		t.Fatal(err)
	}
	if n := m4Posts(t, b.InboxPath()); n != 0 {
		t.Fatalf("baseline emitted %d", n)
	}
	if err := os.WriteFile(path, []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	if err := inbound.DeliverFeeds(b, t0.Add(10*time.Millisecond), debounce); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(secret+"MORE"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := inbound.DeliverFeeds(b, t0.Add(20*time.Millisecond), debounce); err != nil {
		t.Fatal(err)
	}
	if n := m4Posts(t, b.InboxPath()); n != 0 {
		t.Fatalf("inside debounce %d", n)
	}
	if err := inbound.DeliverFeeds(b, t0.Add(20*time.Millisecond+debounce), debounce); err != nil {
		t.Fatal(err)
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	ev, ok := box.Head()
	if !ok {
		t.Fatal("no watch event")
	}
	if !strings.Contains(ev.Content, path) || !strings.Contains(ev.Content, "modified") || strings.Contains(ev.Content, secret) {
		t.Fatalf("metadata event:\n%s", ev.Content)
	}
	k := routineKernel(b, client)
	k.Now = func() time.Time { return t0 }
	k.Inbox = box
	if err := proc.Drain(context.Background(), k); err != nil {
		t.Fatal(err)
	}
	notes := m4Notes(t, b)
	if len(notes) != 1 || notes[0].Text != "file changed" {
		t.Fatalf("notes %+v", notes)
	}
	if !m4Wrapped(t, b, `source="watch:notes"`) {
		t.Fatal("watch wake was not wrapped")
	}
	ents := joinedEntries(t, b)
	if strings.Contains(ents, secret) {
		t.Fatal("file contents reached the transcript")
	}
}

func m4Bot(t *testing.T, root, name string) (*bot.Bot, *provider.Client) {
	t.Helper()
	srv := httptest.NewServer(mockopenai.NewMux(t.TempDir()))
	t.Cleanup(srv.Close)
	client := &provider.Client{BaseURL: srv.URL + "/v1", APIKey: "test", Model: "mock", HTTPClient: srv.Client()}
	return mustBot(t, root, name), client
}

func m4Hook(t *testing.T, b *bot.Bot, name, auth string) inbound.Hook {
	t.Helper()
	h, err := inbound.AddHook(b, name, auth, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func m4Server(root string) (*inbound.Server, time.Time) {
	start := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	s := &inbound.Server{Root: root, Window: time.Second, Now: func() time.Time { return start }}
	return s, start
}

func m4Post(s *inbound.Server, path string, body []byte, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func m4Drain(t *testing.T, b *bot.Bot, client *provider.Client) {
	t.Helper()
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	k := routineKernel(b, client)
	k.Now = func() time.Time { return time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC) }
	k.Inbox = box
	if err := proc.Drain(context.Background(), k); err != nil {
		t.Fatal(err)
	}
}

func m4Notes(t *testing.T, b *bot.Bot) []notify.Note {
	t.Helper()
	notes, err := notify.Open(b.OutboxPath()).Unread()
	if err != nil {
		t.Fatal(err)
	}
	return notes
}

func m4Posts(t *testing.T, path string) int {
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

func m4Wrapped(t *testing.T, b *bot.Bot, source string) bool {
	t.Helper()
	return strings.Contains(joinedEntries(t, b), "<untrusted_event") && strings.Contains(joinedEntries(t, b), source)
}

func joinedEntries(t *testing.T, b *bot.Bot) string {
	t.Helper()
	ents, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	for _, e := range ents {
		buf.WriteString(e.Message.Text())
		buf.WriteByte('\n')
	}
	return buf.String()
}
