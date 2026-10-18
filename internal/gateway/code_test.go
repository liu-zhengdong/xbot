package gateway

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Time-dependent short-code rules the conformance suite cannot run live:
// retention (410 for an hour, then 404), the per-owner registration gap, a
// pending joiner re-pointed at a second owner, and the per-IP failure limit.
func TestCodeRetentionAndLimits(t *testing.T) {
	now := time.Now()
	s := &Server{Dir: t.TempDir(), Token: "join", Now: func() time.Time { return now }}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	client := func() *Client {
		_, key, _ := ed25519.GenerateKey(nil)
		return &Client{Base: srv.URL, Key: key, HTTP: srv.Client(), Now: s.Now}
	}
	check := func(err error, status int) {
		t.Helper()
		var he *HTTPError
		if !(status == 200 && err == nil) && (!errors.As(err, &he) || he.StatusCode != status) {
			t.Fatalf("want %d, got %v", status, err)
		}
	}
	ctx := context.Background()
	a, c, b := client(), client(), client()
	check(a.Register(ctx, "join"), 200)
	check(c.Register(ctx, "join"), 200)
	check(a.RegisterCode(ctx, "K7QM", time.Minute), 200)
	check(a.RegisterCode(ctx, "ABCD", time.Minute), 429) // Within the 2 s gap.
	check(c.RegisterCode(ctx, "WXYZ", time.Minute), 200)

	_, err := b.ClaimCode(ctx, "K7QM")
	check(err, 200)
	owner, err := b.ClaimCode(ctx, "WXYZ") // Re-pointed at c.
	check(err, 200)
	if owner != ID(c.Key.Public().(ed25519.PublicKey)) {
		t.Fatalf("owner %q", owner)
	}
	check(b.Send(ctx, NewEnvelope(b.Key, ID(a.Key.Public().(ed25519.PublicKey)), "pake1", nil, now)), 403)

	now = now.Add(2 * time.Minute)
	_, err = client().ClaimCode(ctx, "K7QM")
	check(err, 410)
	now = now.Add(time.Hour)
	_, err = client().ClaimCode(ctx, "K7QM")
	check(err, 404)

	for i := 0; i < 9; i++ { // The 404 above + 9 = the 10-per-minute limit.
		_, err = client().ClaimCode(ctx, "ZZZZ")
		check(err, 404)
	}
	_, err = client().ClaimCode(ctx, "ZZZZ")
	check(err, 429)

	// IPv6 is limited per /64: two addresses in one /64 share the budget.
	now = now.Add(2 * time.Minute)
	h := s.Handler()
	for i := range 11 {
		c := client()
		c.HTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			r.RemoteAddr = fmt.Sprintf("[2001:db8::%d]:1", i)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			return w.Result(), nil
		})}
		_, err = c.ClaimCode(ctx, "ZZZZ")
		check(err, map[bool]int{true: 429, false: 404}[i == 10])
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
