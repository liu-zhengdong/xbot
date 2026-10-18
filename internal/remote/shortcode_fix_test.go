package remote

import (
	"context"
	"testing"
	"time"
)

// Review regressions: a retry from B, a late allow, clock skew, the fifth session.
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
		sb.join.expires = sb.join.expires.Add(3 * time.Minute) // B set it by its own fast clock.
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
}
