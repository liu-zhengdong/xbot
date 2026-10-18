package remote

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"xbot/internal/gateway"
)

// Review regressions: a retry from B, a late allow, clock skew, the fifth session, a lost pake3 response.
func TestShortCodeRetryLateAndSkew(t *testing.T) {
	ctx := context.Background()
	t.Run("retry", func(t *testing.T) {
		// B gave up and retried before A answered: A starts a new session for
		// the same joiner, and B ignores the answer to the first attempt
		// instead of calling a correct code wrong.
		h := newShortHarness(t)
		mustRemote(t, JoinShortCode(ctx, h.b, h.code, h.ca.Base))
		mustRemote(t, JoinShortCode(ctx, h.b, h.code, h.ca.Base))
		h.deliver(h.a, h.ca)
		h.deliver(h.b, h.cb)
		if got := PairingStatus(h.b).B.Phase; got != "confirm" {
			t.Fatal("retry ended in", got)
		}
	})
	t.Run("late allow", func(t *testing.T) {
		// A's allow arrives after B's window: A already saved B, so B must too.
		h := newShortHarness(t)
		h.request()
		tickPairing(ctx, h.b, h.cb, time.Now().Add(2*time.Minute+16*time.Second))
		if PairingStatus(h.b).B.Phase != "denied" {
			t.Fatal("B window did not end")
		}
		mustRemote(t, DecidePairing(ctx, h.a, true, false))
		h.deliver(h.b, h.cb)
		cfg, err := Load(h.b)
		mustRemote(t, err)
		if PairingStatus(h.b).B.Phase != "done" || len(cfg.Devices) != 1 {
			t.Fatal("late allow dropped:", PairingStatus(h.b).B)
		}
	})
	t.Run("skew", func(t *testing.T) {
		// B's clock runs 3 minutes fast (envelopes allow 5): A's 2-minute
		// deadline is measured on A's clock, so B still offers 连接.
		h := newShortHarness(t)
		mustRemote(t, JoinShortCode(ctx, h.b, h.code, h.ca.Base))
		h.deliver(h.a, h.ca)
		_, self, _, err := Identity(h.b)
		mustRemote(t, err)
		envs := h.poll(h.cb)
		sb := pairingFor(h.b)
		sb.Lock()
		sb.join.expires = sb.join.expires.Add(3 * time.Minute) // B set these by its own fast clock.
		sb.join.sent1 = sb.join.sent1.Add(3 * time.Minute)
		sb.Unlock()
		_, _, err = Receive(h.b, self, envs, time.Now().Add(3*time.Minute))
		mustRemote(t, err)
		got := PairingStatus(h.b).B
		if got.Phase != "confirm" || got.Expires.After(time.Now().Add(3*time.Minute+2*time.Minute)) {
			t.Fatalf("skewed B: %+v", got)
		}
	})
	t.Run("fifth session", func(t *testing.T) {
		// The code dies with the fifth session, but A keeps showing it as
		// live until that session ends, so 换一个 does not cut it off.
		h := newShortHarness(t)
		s := pairingFor(h.a)
		s.Lock()
		s.owner.sessions = 4
		s.Unlock()
		mustRemote(t, JoinShortCode(ctx, h.b, h.code, h.ca.Base))
		h.deliver(h.a, h.ca)
		if got := PairingStatus(h.a).A.Phase; got != "waiting" {
			t.Fatal("fifth session shown as", got)
		}
		tickPairing(ctx, h.a, h.ca, time.Now().Add(2*time.Minute+time.Second))
		if got := PairingStatus(h.a).A.Phase; got != "expired" {
			t.Fatal("after the fifth session:", got)
		}
	})
	t.Run("lost pake3 response", func(t *testing.T) {
		// The gateway queued B's 连接 but the response was lost: B still shows
		// 连接, and A's allow must pair both sides, not only A.
		h := newShortHarness(t)
		h.join()
		old := http.DefaultClient
		http.DefaultClient = &http.Client{Transport: pairTransport(func(r *http.Request) (*http.Response, error) {
			if resp, err := old.Transport.RoundTrip(r); err == nil { // The harness gateway queues it.
				resp.Body.Close()
			}
			return nil, errors.New("connection reset")
		})}
		err := ConfirmJoin(ctx, h.b, "")
		http.DefaultClient = old
		if err == nil || PairingStatus(h.b).B.Phase != "confirm" {
			t.Fatal("lost response not surfaced:", err, PairingStatus(h.b).B.Phase)
		}
		h.deliver(h.a, h.ca)
		mustRemote(t, DecidePairing(ctx, h.a, true, false))
		h.deliver(h.b, h.cb)
		if got := PairingStatus(h.b).B.Phase; got != "done" {
			t.Fatal("allow after a lost response:", got)
		}
	})
}

// A late allow must not pair or overwrite a cancelled or superseded attempt.
func TestShortCodeLateAllowForgotten(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"cancel", "set phase", "rejoin"} {
		t.Run(mode, func(t *testing.T) {
			h := newShortHarness(t)
			h.request()
			// B's window ends, so the request is kept as late.
			tickPairing(ctx, h.b, h.cb, time.Now().Add(2*time.Minute+16*time.Second))
			switch mode {
			case "cancel":
				CancelJoin(h.b)
			case "set phase":
				SetJoinPhase(h.b, PairingPhase{Phase: "gateway_unreachable"})
			case "rejoin":
				// Any new attempt supersedes the late one, even a mistyped code.
				_ = JoinShortCode(ctx, h.b, "short", h.ca.Base)
			}
			before := PairingStatus(h.b).B
			mustRemote(t, DecidePairing(ctx, h.a, true, false))
			h.deliver(h.b, h.cb)
			if got := PairingStatus(h.b).B; got.Phase != before.Phase {
				t.Fatalf("B overwritten: %+v", got)
			}
			cfg, err := Load(h.b)
			mustRemote(t, err)
			if len(cfg.Devices) != 0 {
				t.Fatal("late allow overwrote B")
			}
		})
	}
}

// A pake2 relayed 15 s late must not keep B offering 连接 past A's deadline:
// B's window shrinks by the time it waited (its own clock).
func TestShortCodeLatePake2ShortensWindow(t *testing.T) {
	window := func(delay time.Duration) time.Duration {
		h := newShortHarness(t)
		mustRemote(t, JoinShortCode(context.Background(), h.b, h.code, h.ca.Base))
		h.deliver(h.a, h.ca)
		_, self, _, err := Identity(h.b)
		mustRemote(t, err)
		envs := h.poll(h.cb)
		now := time.Now().Add(delay)
		_, _, err = Receive(h.b, self, envs, now)
		mustRemote(t, err)
		got := PairingStatus(h.b).B
		if got.Phase != "confirm" {
			t.Fatalf("delay %v: %+v", delay, got)
		}
		return got.Expires.Sub(now)
	}
	prompt, late := window(0), window(15*time.Second)
	if d := prompt - late; d < 14*time.Second || d > 16*time.Second {
		t.Fatalf("window %v on time, %v when 15 s late", prompt, late)
	}
}

// Only a saved device may be reported done.
func TestShortCodeUnsavedNotDone(t *testing.T) {
	yes := true
	if p := (&shortSession{result: &yes}).finalPhase(); p != "denied" {
		t.Fatal("unsaved allow reported as", p)
	}
	if p := (&shortSession{result: &yes, saved: true}).finalPhase(); p != "done" {
		t.Fatal("saved allow reported as", p)
	}
}

// The gateway's 429 on a code is its own error and B phase, never 连不上网关.
func TestShortCodeRateLimited(t *testing.T) {
	h := newShortHarness(t)
	old := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: pairTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/v1/code" || r.URL.Path == "/v1/code/claim" {
			return &http.Response{StatusCode: 429, Status: "429 Too Many Requests", Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
		}
		base := old.Transport
		if base == nil {
			base = http.DefaultTransport
		}
		return base.RoundTrip(r)
	})}
	defer func() { http.DefaultClient = old }()
	// Once is a retry after the gateway's 2 s gap; 429 again gives up on the code.
	if _, _, err := NewShortCode(context.Background(), h.a, ""); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("NewShortCode on 429: %v", err)
	}
	var he *gateway.HTTPError
	if err := JoinShortCode(context.Background(), h.b, h.code, h.ca.Base); !errors.As(err, &he) || he.StatusCode != 429 {
		t.Fatalf("JoinShortCode on 429: %v", err)
	}
	if got := PairingStatus(h.b).B; got.Phase != "rate_limited" {
		t.Fatalf("B on 429: %+v", got)
	}
}

// With every "Café laptop 1"…"Café laptop 9" label taken by other installs, a save
// says ErrNameFull: A's allow reports it, and on B it is a phase, not 没有允许.
func TestShortCodeNameFull(t *testing.T) {
	h := newShortHarness(t)
	for n := 1; n <= 9; n++ {
		pub, _, err := ed25519.GenerateKey(rand.Reader)
		mustRemote(t, err)
		_, err = AddDevice(h.a, fmt.Sprintf("filler-%d", n), base64.StdEncoding.EncodeToString(pub), base64.StdEncoding.EncodeToString(make([]byte, 32)), "u1")
		mustRemote(t, err)
		label := "Café laptop"
		if n > 1 {
			label = fmt.Sprintf("Café laptop %d", n)
		}
		i := n - 1
		mustRemote(t, update(h.a, func(c *Config) error { c.Devices[i].Label = label; return nil }))
	}
	h.request()
	if err := DecidePairing(context.Background(), h.a, true, false); !errors.Is(err, ErrNameFull) {
		t.Fatalf("allow with a full name: %v", err)
	}
	if cfg, err := Load(h.a); err != nil || len(cfg.Devices) != 9 {
		t.Fatalf("A saved the joiner anyway: %v", err)
	}
	// The card stays (a tick does not turn it into a deny) and 替换旧的 still works.
	tickPairing(context.Background(), h.a, h.ca, time.Now())
	if got := PairingStatus(h.a).A.Phase; got != "request" {
		t.Fatalf("tick after a full name: %s", got)
	}
	mustRemote(t, DecidePairing(context.Background(), h.a, true, true))
	if got := PairingStatus(h.a).A.Phase; got != "done" {
		t.Fatalf("replace after a full name: %s", got)
	}

	// B's own side of the same save: A allowed, but B has no free label.
	other := newShortHarness(t)
	mustRemote(t, SetGateway(other.b, "http://gateway"))
	for n := 1; n <= 9; n++ {
		pub, _, err := ed25519.GenerateKey(rand.Reader)
		mustRemote(t, err)
		_, err = AddDevice(other.b, fmt.Sprintf("filler-%d", n), base64.StdEncoding.EncodeToString(pub), base64.StdEncoding.EncodeToString(make([]byte, 32)), "")
		mustRemote(t, err)
		label := "书房 iMac"
		if n > 1 {
			label = fmt.Sprintf("书房 iMac %d", n)
		}
		i := n - 1
		mustRemote(t, update(other.b, func(c *Config) error { c.Devices[i].Label = label; return nil }))
	}
	other.request()
	mustRemote(t, DecidePairing(context.Background(), other.a, true, false))
	other.deliver(other.b, other.cb)
	if got := PairingStatus(other.b).B.Phase; got != "name_full" {
		t.Fatalf("B with a full name: %s", got)
	}
}

// An expired code with no session left must not keep Run polling for ever.
func TestShortCodeExpiredOwnerInactive(t *testing.T) {
	h := newShortHarness(t)
	if !pairingActive(h.a) {
		t.Fatal("live code not active")
	}
	s := pairingFor(h.a)
	s.Lock()
	s.owner.expires = time.Now().Add(-time.Second)
	s.release()
	if pairingActive(h.a) {
		t.Fatal("expired code still active")
	}
	s.Lock()
	s.owner.active = &shortSession{expires: time.Now().Add(time.Minute)}
	s.release()
	if !pairingActive(h.a) {
		t.Fatal("active session not counted")
	}
}
