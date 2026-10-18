package remote

import (
	"context"
	"testing"
	"time"

	"xbot/internal/gateway"
)

// A pairing request must reach the desktop app exactly once, not on every poll
// of the same request and not again after the answer, however often A is polled.
func TestPairRequestNotifiesOnce(t *testing.T) {
	h := newShortHarness(t)
	got := make(chan string, 4)
	OnPairRequest = func(root, name string) { got <- root + "|" + name }
	t.Cleanup(func() { OnPairRequest = nil })
	h.request()
	waitFor := func() {
		t.Helper()
		select {
		case v := <-got:
			if want := h.a + "|Café laptop"; v != want {
				t.Fatalf("OnPairRequest(%q), want %q", v, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("owner request did not reach OnPairRequest")
		}
	}
	waitFor()
	// The redelivered pake3 and later polls of the same request stay silent.
	envs, err := h.ca.Poll(context.Background(), 0)
	mustRemote(t, err)
	for _, env := range envs {
		_, self, _, idErr := Identity(h.a)
		mustRemote(t, idErr)
		_, _, _ = Receive(h.a, self, []gateway.Envelope{env}, time.Now())
	}
	for range 3 {
		tickPairing(context.Background(), h.a, h.ca, time.Now())
	}
	mustRemote(t, DecidePairing(context.Background(), h.a, false, false))
	tickPairing(context.Background(), h.a, h.ca, time.Now().Add(3*time.Minute))
	select {
	case v := <-got:
		t.Fatalf("OnPairRequest called again with %q", v)
	case <-time.After(100 * time.Millisecond):
	}
}
