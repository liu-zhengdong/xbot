package gateway

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Key path: a pending joiner (no gateway token) can only use its mailbox and
// talk to its owner until the owner admits it, then it is a full member.
func TestPendingInstall(t *testing.T) {
	now := time.Now()
	s := &Server{Dir: t.TempDir(), Token: "join", Now: func() time.Time { return now }}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	client := func() *Client {
		_, key, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		return &Client{Base: srv.URL, Key: key, HTTP: srv.Client(), Now: s.Now}
	}
	id := func(c *Client) string { return ID(c.Key.Public().(ed25519.PublicKey)) }
	check := func(err error, status int) {
		t.Helper()
		var he *HTTPError
		if status == 200 && err == nil {
			return
		}
		if !errors.As(err, &he) || he.StatusCode != status {
			t.Fatalf("want %d, got %v", status, err)
		}
	}
	ctx := context.Background()
	a, b, c := client(), client(), client()
	check(a.Register(ctx, "join"), 200)
	check(c.Register(ctx, "join"), 200)
	check(b.Send(ctx, NewEnvelope(b.Key, id(a), "pake1", nil, now)), 403) // Not pending yet.
	s.mu.Lock()
	err := s.admitPendingLocked(b.Key.Public().(ed25519.PublicKey), id(a))
	s.mu.Unlock()
	check(err, 200)

	// Pending: mailbox and owner only.
	_, err = b.Poll(ctx, 0)
	check(err, 200)
	check(b.Send(ctx, NewEnvelope(b.Key, id(a), "pake1", nil, now)), 200)
	check(b.Send(ctx, NewEnvelope(b.Key, id(c), "msg", nil, now)), 403)
	check(c.Send(ctx, NewEnvelope(c.Key, id(b), "msg", nil, now)), 403)
	check(a.Send(ctx, NewEnvelope(a.Key, id(b), "pake2", nil, now)), 200)
	check(b.Register(ctx, ""), 403)
	check(b.AdmitCode(ctx, id(c)), 403)
	envs, err := b.Poll(ctx, 0)
	check(err, 200)
	if len(envs) != 1 || envs[0].Kind != "pake2" {
		t.Fatalf("pending mailbox: %+v", envs)
	}
	check(b.Ack(ctx, []string{envs[0].ID}), 200)

	// Only the owner admits; admit is idempotent once B is a member.
	check(c.AdmitCode(ctx, id(b)), 404)
	check(a.AdmitCode(ctx, id(b)), 200)
	check(a.AdmitCode(ctx, id(b)), 200)
	check(a.AdmitCode(ctx, strings.Repeat("e", 32)), 404)
	check(b.Register(ctx, ""), 200) // Known key: tokenless re-register.
	check(b.Send(ctx, NewEnvelope(b.Key, id(c), "msg", nil, now)), 200)
	if _, err := os.Stat(filepath.Join(s.Dir, "pending", id(b))); !os.IsNotExist(err) {
		t.Fatalf("admitted install still pending: %v", err)
	}

	// An unadmitted pending install lapses with its mail.
	d := client()
	s.mu.Lock()
	err = s.admitPendingLocked(d.Key.Public().(ed25519.PublicKey), id(a))
	s.mu.Unlock()
	check(err, 200)
	check(d.Send(ctx, NewEnvelope(d.Key, id(a), "pake1", nil, now)), 200)
	now = now.Add(PendingTTL + 2*time.Minute)
	_, err = d.Poll(ctx, 0)
	check(err, 403)
	check(a.AdmitCode(ctx, id(d)), 404)
	queued, err := filepath.Glob(filepath.Join(s.Dir, "queue", id(a), id(d), "*"))
	if err != nil || len(queued) != 0 {
		t.Fatalf("lapsed pending left mail: %v %v", queued, err)
	}
}
