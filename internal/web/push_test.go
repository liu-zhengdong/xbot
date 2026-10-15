package web

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/notify"
)

func TestPhonePushSkipsOpenTabAndQuietHours(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "persona", []string{link.NameTerminal})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	hits := 0
	pushSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		if r.Header.Get("Content-Encoding") != "aes128gcm" || r.Header.Get("Authorization") == "" {
			t.Errorf("headers %v", r.Header)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer pushSrv.Close()
	if err := notify.SaveSubscription(root, "u1", testPushSub(t, pushSrv.URL+"/p")); err != nil {
		t.Fatal(err)
	}
	s.pushHTTP = pushSrv.Client()
	api := httptest.NewServer(muxOf(s))
	defer api.Close()

	if _, err := notify.Open(b.OutboxPath()).Append("phone hello", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatalf("closed tab hits %d", hits)
	}

	res := getJSON(t, api.URL+"/api/notes", tok)
	var notes struct {
		Notes []struct {
			Text string `json:"text"`
		} `json:"notes"`
	}
	if err := json.Unmarshal(res, &notes); err != nil {
		t.Fatal(err)
	}
	if len(notes.Notes) != 1 || notes.Notes[0].Text != "phone hello" {
		t.Fatalf("notes %+v", notes.Notes)
	}
	if _, err := notify.Open(b.OutboxPath()).Append("while open", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatalf("open tab pushed, hits %d", hits)
	}

	s.mu.Lock()
	s.tabs["u1"].n = 0
	s.tabs["u1"].last = time.Now().Add(-time.Minute)
	s.mu.Unlock()
	if _, err := notify.Open(b.OutboxPath()).Append("after close", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if hits != 2 {
		t.Fatalf("reclosed hits %d", hits)
	}

	start := time.Now().UTC().Add(-time.Hour).Format("15:04")
	end := time.Now().UTC().Add(time.Hour).Format("15:04")
	zone := "UTC"
	if err := b.ApplyProfile(bot.ProfileUpdate{QuietStart: &start, QuietEnd: &end, Timezone: &zone}); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.tabs["u1"].n = 0
	s.tabs["u1"].last = time.Now().Add(-time.Minute)
	s.mu.Unlock()
	if _, err := notify.Open(b.OutboxPath()).Append("during quiet", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if hits != 2 {
		t.Fatalf("quiet hours pushed, hits %d", hits)
	}

	if err := notify.Hold(b.HeldPath(), "held one"); err != nil {
		t.Fatal(err)
	}
	if err := notify.Hold(b.HeldPath(), "held two"); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.tabs["u1"].n = 0
	s.tabs["u1"].last = time.Now()
	s.mu.Unlock()
	off := ""
	if err := b.ApplyProfile(bot.ProfileUpdate{QuietStart: &off, QuietEnd: &off}); err != nil {
		t.Fatal(err)
	}
	n, err := kernel.ReleaseQuiet(context.Background(), b, time.Now().UTC(), func(context.Context, string, string) error { return nil })
	if err != nil || n != 2 {
		t.Fatalf("release %d %v", n, err)
	}
	if hits != 3 {
		t.Fatalf("batch while recently open was not pushed, hits %d", hits)
	}
	unread, err := notify.Open(b.OutboxPath()).Unread()
	if err != nil {
		t.Fatal(err)
	}
	var batch string
	for _, note := range unread {
		if notify.IsBatch(note) {
			batch = note.Text
		}
	}
	if !strings.Contains(batch, "held one") || !strings.Contains(batch, "held two") {
		t.Fatalf("page outbox missed the batch %q", batch)
	}

	if err := notify.Hold(b.HeldPath(), "while streaming"); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.tabs["u1"].n = 1
	s.mu.Unlock()
	n, err = kernel.ReleaseQuiet(context.Background(), b, time.Now().UTC(), func(context.Context, string, string) error { return nil })
	if err != nil || n != 1 {
		t.Fatalf("second release %d %v", n, err)
	}
	if hits != 3 {
		t.Fatalf("in-flight page was pushed, hits %d", hits)
	}
}

func TestPushSubscribeAPI(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(muxOf(s))
	defer api.Close()
	status := getJSON(t, api.URL+"/api/push", tok)
	var st struct {
		PublicKey  string `json:"public_key"`
		Subscribed bool   `json:"subscribed"`
	}
	if err := json.Unmarshal(status, &st); err != nil || st.PublicKey == "" || st.Subscribed {
		t.Fatalf("%s %v", status, err)
	}
	sub := testPushSub(t, "https://example.com/device")
	res, err := http.NewRequest(http.MethodPost, api.URL+"/api/push/subscribe", bytes.NewReader(mustJSON(t, sub)))
	if err != nil {
		t.Fatal(err)
	}
	res.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
	got, err := api.Client().Do(res)
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	if got.StatusCode != 200 {
		raw, _ := io.ReadAll(got.Body)
		t.Fatalf("subscribe %d %s", got.StatusCode, raw)
	}
	status = getJSON(t, api.URL+"/api/push", tok)
	if err := json.Unmarshal(status, &st); err != nil || !st.Subscribed {
		t.Fatalf("after %s %v", status, err)
	}
	req, _ := http.NewRequest(http.MethodDelete, api.URL+"/api/push/subscribe", bytes.NewReader(mustJSON(t, map[string]string{})))
	req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
	del, err := api.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer del.Body.Close()
	if del.StatusCode != 200 {
		t.Fatalf("delete %d", del.StatusCode)
	}
	left, err := notify.Subscriptions(root, "u1")
	if err != nil || len(left) != 0 {
		t.Fatalf("left %+v %v", left, err)
	}
}

func TestPushDropsGoneSubscription(t *testing.T) {
	root := t.TempDir()
	s := &Server{Root: root, Owner: "u1"}
	if _, _, err := s.EnsureToken(); err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	pushSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer pushSrv.Close()
	if err := notify.SaveSubscription(root, "u1", testPushSub(t, pushSrv.URL+"/gone")); err != nil {
		t.Fatal(err)
	}
	s.pushHTTP = pushSrv.Client()
	muxOf(s)
	if _, err := notify.Open(b.OutboxPath()).Append("stale", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	left, err := notify.Subscriptions(root, "u1")
	if err != nil || len(left) != 0 {
		t.Fatalf("left %+v %v", left, err)
	}
}

func testPushSub(t *testing.T, endpoint string) notify.Subscription {
	t.Helper()
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatal(err)
	}
	return notify.Subscription{
		Endpoint: endpoint,
		P256dh:   base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()),
		Auth:     base64.RawURLEncoding.EncodeToString(auth),
	}
}

func getJSON(t *testing.T, url, tok string) []byte {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: cookieName, Value: tok})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("%s %d %s", url, res.StatusCode, raw)
	}
	return raw
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
