package remote

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
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
