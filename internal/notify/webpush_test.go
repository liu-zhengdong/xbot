package notify

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPushRoundTrip(t *testing.T) {
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatal(err)
	}
	sub := Subscription{
		Endpoint: "https://push.example/send/1",
		P256dh:   b64enc(priv.PublicKey().Bytes()),
		Auth:     b64enc(auth),
	}
	want := []byte(`{"title":"ada","body":"hello"}`)
	body, err := encryptPush(sub, want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decryptPush(body, priv.Bytes(), auth)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("got %s", got)
	}
}

func TestSendUsesVAPID(t *testing.T) {
	root := t.TempDir()
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatal(err)
	}
	var gotEnc, gotAuth string
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEnc = r.Header.Get("Content-Encoding")
		gotAuth = r.Header.Get("Authorization")
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	sub := Subscription{
		Endpoint: srv.URL + "/push",
		P256dh:   b64enc(priv.PublicKey().Bytes()),
		Auth:     b64enc(auth),
	}
	if err := Send(context.Background(), root, sub, Message{Title: "ada", Body: "shipped", Tag: "nt_abc"}, srv.Client()); err != nil {
		t.Fatal(err)
	}
	if gotEnc != "aes128gcm" || !strings.HasPrefix(gotAuth, "vapid t=") || !strings.Contains(gotAuth, ",k=") {
		t.Fatalf("enc %q auth %q", gotEnc, gotAuth)
	}
	plain, err := decryptPush(body, priv.Bytes(), auth)
	if err != nil || !strings.Contains(string(plain), "shipped") {
		t.Fatalf("plain %q %v", plain, err)
	}
	fi, err := os.Stat(filepath.Join(root, "push", "vapid.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Fatalf("mode %o", fi.Mode().Perm())
	}
}

func TestGoneDropsMeaning(t *testing.T) {
	root := t.TempDir()
	sub := mustSub(t, "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer srv.Close()
	sub.Endpoint = srv.URL + "/push"
	err := Send(context.Background(), root, sub, Message{Title: "ada", Body: "x"}, srv.Client())
	if !Gone(err) {
		t.Fatalf("err %v", err)
	}
}

func TestSubscriptionRejects(t *testing.T) {
	root := t.TempDir()
	sub := mustSub(t, "http://example.com/push")
	if err := SaveSubscription(root, "u1", sub); err == nil {
		t.Fatal("http endpoint accepted")
	}
	sub.Endpoint = "https://127.0.0.1/push"
	if err := SaveSubscription(root, "u1", sub); err == nil {
		t.Fatal("loopback https accepted")
	}
	sub.Endpoint = "https://10.1.2.3/push"
	if err := SaveSubscription(root, "u1", sub); err == nil {
		t.Fatal("private https accepted")
	}
	sub.Endpoint = "https://push.example/s"
	if err := SaveSubscription(root, "../x", sub); err == nil {
		t.Fatal("bad owner accepted")
	}
	if err := SaveSubscription(root, "u1", sub); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(root, "push", "subs", "u1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Fatalf("mode %o", fi.Mode().Perm())
	}
	again := sub
	again.Auth = mustSub(t, "https://push.example/s").Auth
	if err := SaveSubscription(root, "u1", again); err != nil {
		t.Fatal(err)
	}
	got, err := Subscriptions(root, "u1")
	if err != nil || len(got) != 1 || got[0].Auth != again.Auth {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestHoldDoesNotEmit(t *testing.T) {
	root := t.TempDir()
	n := 0
	Bind(root, func(string, Note) { n++ })
	t.Cleanup(func() { Bind(root, nil) })
	if err := Hold(filepath.Join(root, "held.json"), "during quiet"); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("hold emitted %d", n)
	}
	if _, err := Open(filepath.Join(root, "bots", "ada", "outbox.jsonl")).Append("after", time.Now()); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("append emitted %d", n)
	}
}

func mustSub(t *testing.T, endpoint string) Subscription {
	t.Helper()
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatal(err)
	}
	if endpoint == "" {
		endpoint = "https://push.example/s"
	}
	return Subscription{
		Endpoint: endpoint,
		P256dh:   b64enc(priv.PublicKey().Bytes()),
		Auth:     b64enc(auth),
	}
}
