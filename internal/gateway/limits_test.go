package gateway

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// limitClient returns a client whose requests arrive from ip behind a proxy.
func limitClient(t *testing.T, s *Server, ip string) *Client {
	t.Helper()
	h := s.Handler()
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	hc := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		r.Header.Set("X-Forwarded-For", "203.0.113.250, "+ip) // Only the last hop counts.
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Result(), nil
	})}
	return &Client{Base: "http://gateway", Key: key, HTTP: hc, Now: s.Now}
}

func wantStatus(t *testing.T, err error, status int) {
	t.Helper()
	var he *HTTPError
	if !(status == 200 && err == nil) && (!errors.As(err, &he) || he.StatusCode != status) {
		t.Fatalf("want %d, got %v", status, err)
	}
}

// The official gateway raises the install cap; the 65th install then fits,
// and a full gateway still refuses with 403.
func TestInstallCapOverride(t *testing.T) {
	s := &Server{Dir: t.TempDir(), Open: true, TrustForwarded: true, InstallCap: MaxInstalls + 2, DailyPerIP: -1}
	ctx := context.Background()
	for i := 0; i < MaxInstalls+2; i++ {
		wantStatus(t, limitClient(t, s, "198.51.100.1").Register(ctx, ""), 200)
	}
	wantStatus(t, limitClient(t, s, "198.51.100.1").Register(ctx, ""), 403)
}

// Like the Worker: 30 new installs per IP per UTC day, re-registration is
// free, another IP has its own bucket, and the next UTC day starts over.
func TestDailyInstallsPerIP(t *testing.T) {
	now := time.Date(2026, 10, 26, 23, 0, 0, 0, time.UTC)
	s := &Server{Dir: t.TempDir(), Open: true, TrustForwarded: true, Now: func() time.Time { return now }}
	ctx := context.Background()
	var first *Client
	for i := 0; i < DailyInstallsPerIP; i++ {
		c := limitClient(t, s, "198.51.100.7")
		wantStatus(t, c.Register(ctx, ""), 200)
		if first == nil {
			first = c
		}
	}
	wantStatus(t, limitClient(t, s, "198.51.100.7").Register(ctx, ""), 429)
	wantStatus(t, first.Register(ctx, ""), 200) // Known install: no bucket.
	wantStatus(t, limitClient(t, s, "198.51.100.8").Register(ctx, ""), 200)
	// IPv6 counts per /64: another host in the same /64 shares the bucket.
	for i := 0; i < DailyInstallsPerIP; i++ {
		wantStatus(t, limitClient(t, s, "2001:db8:1:2::"+string(rune('a'+i%6))).Register(ctx, ""), 200)
	}
	wantStatus(t, limitClient(t, s, "2001:db8:1:2:ffff::1").Register(ctx, ""), 429)
	now = now.Add(2 * time.Hour) // Next UTC day.
	wantStatus(t, limitClient(t, s, "198.51.100.7").Register(ctx, ""), 200)
}

// A short-code joiner's promotion to member counts against the IP that
// claimed the code, like the Worker's codeAdmit.
func TestCodePromotionCountsClaimantIP(t *testing.T) {
	now := time.Now()
	s := &Server{Dir: t.TempDir(), Token: "join", TrustForwarded: true, Now: func() time.Time { return now }}
	ctx := context.Background()
	owner := limitClient(t, s, "198.51.100.1")
	wantStatus(t, owner.Register(ctx, "join"), 200)
	s.mu.Lock()
	s.dailyFullLocked("198.51.100.9")
	s.regCount["198.51.100.9"] = DailyInstallsPerIP - 1
	s.mu.Unlock()

	claim := func(lookup string) *Client {
		t.Helper()
		now = now.Add(3 * time.Second) // Past the per-owner registration gap.
		wantStatus(t, owner.RegisterCode(ctx, lookup, time.Minute), 200)
		j := limitClient(t, s, "198.51.100.9")
		if _, err := j.ClaimCode(ctx, lookup); err != nil {
			t.Fatal(err)
		}
		return j
	}
	j1 := claim("K7QM")
	wantStatus(t, owner.AdmitCode(ctx, ID(j1.Key.Public().(ed25519.PublicKey))), 200)
	j2 := claim("WXYZ")
	wantStatus(t, owner.AdmitCode(ctx, ID(j2.Key.Public().(ed25519.PublicKey))), 429)
}
