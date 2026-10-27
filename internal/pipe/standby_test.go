package pipe

import (
	"context"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// gate sits between a device and the hub: shut, it hangs up on every
// connection; cut drops the ones it carries.
type gate struct {
	ln     net.Listener
	target string
	mu     sync.Mutex
	open   bool
	tries  int
	conns  []net.Conn
}

func newGate(t *testing.T, hubURL string) *gate {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	g := &gate{ln: ln, target: strings.TrimPrefix(hubURL, "http://")}
	go g.serve()
	t.Cleanup(func() { ln.Close(); g.cut() })
	return g
}

func (g *gate) serve() {
	for {
		c, err := g.ln.Accept()
		if err != nil {
			return
		}
		g.mu.Lock()
		g.tries++
		if !g.open {
			g.mu.Unlock()
			c.Close()
			continue
		}
		u, err := net.Dial("tcp", g.target)
		if err != nil {
			g.mu.Unlock()
			c.Close()
			continue
		}
		g.conns = append(g.conns, c, u)
		g.mu.Unlock()
		go func() { _, _ = io.Copy(u, c); u.Close(); c.Close() }()
		go func() { _, _ = io.Copy(c, u); u.Close(); c.Close() }()
	}
}

func (g *gate) set(open bool) { g.mu.Lock(); g.open = open; g.mu.Unlock() }

func (g *gate) attempts() int { g.mu.Lock(); defer g.mu.Unlock(); return g.tries }

func (g *gate) cut() {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, c := range g.conns {
		c.Close()
	}
	g.conns = nil
}

// The device reports standby only once the hub acknowledges it: a refused
// handshake or an unreachable hub says nothing, a dropped link says down,
// and the next acknowledged standby says up again.
func TestStandbyFollowsHub(t *testing.T) {
	w := newWorld(t)
	run := func(d *Device) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); _ = d.Run(ctx) }()
		t.Cleanup(func() { cancel(); <-done })
	}
	waitFor := func(ok func() bool, msg string) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
			if ok() {
				return
			}
		}
		t.Fatal(msg)
	}

	// The hub refuses the handshake: never up.
	refusedLog := &lockedBuf{}
	refused := make(chan bool, 4)
	run(&Device{Hub: w.hubURL, Session: "not-a-session", DeviceID: "mini-1", Handler: w.device.Handler, Log: log.New(refusedLog, "", 0), Standby: func(up bool) { refused <- up }})
	waitFor(func() bool { return strings.Contains(refusedLog.String(), "standby ended") }, "refused handshake never failed")
	if len(refused) != 0 {
		t.Fatalf("refused handshake reported standby: %v", <-refused)
	}

	g := newGate(t, w.hubURL)
	events := make(chan bool, 16)
	d := *w.device
	d.Hub = "http://" + g.ln.Addr().String()
	d.Standby = func(up bool) { events <- up }
	next := func(want bool, msg string) {
		t.Helper()
		select {
		case up := <-events:
			if up != want {
				t.Fatalf("%s: got %v", msg, up)
			}
		case <-time.After(10 * time.Second):
			t.Fatal(msg)
		}
	}
	run(&d)

	// The hub cannot be reached: nothing.
	waitFor(func() bool { return g.attempts() >= 1 }, "device never dialled")
	time.Sleep(50 * time.Millisecond)
	if len(events) != 0 {
		t.Fatalf("unreachable hub reported standby: %v", <-events)
	}

	g.set(true)
	next(true, "standby never came up")
	w.hub.mu.Lock()
	held := len(w.hub.standby)
	w.hub.mu.Unlock()
	if held != 1 {
		t.Fatalf("up before the hub holds the standby: %d", held)
	}

	g.cut()
	next(false, "dropped link not reported")
	next(true, "standby did not recover")
}
