package remote

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// A short-code joiner is a pending install: register answers 403 until the
// owner allows, yet its loop must start as soon as the gateway is set and keep
// polling for the owner's replies instead of backing off.
func TestRunWakesAndPollsWhileNotAdmitted(t *testing.T) {
	var registers, polls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/register":
			registers.Add(1)
			http.Error(w, "not admitted", http.StatusForbidden)
		case "/v1/poll":
			polls.Add(1)
			time.Sleep(20 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"envelopes":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	root := pairingRoot(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { Run(ctx, root, "", nil); close(done) }()
	defer func() { cancel(); <-done }()
	time.Sleep(100 * time.Millisecond) // Run is idling in its 30 s no-gateway pause.
	mustRemote(t, SetGateway(root, srv.URL))
	deadline := time.Now().Add(5 * time.Second)
	for polls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if polls.Load() < 3 || registers.Load() < 3 {
		t.Fatalf("polls %d, registers %d: loop did not wake or backed off on 403", polls.Load(), registers.Load())
	}
}
