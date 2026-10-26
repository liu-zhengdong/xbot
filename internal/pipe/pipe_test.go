package pipe

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/account"
	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/web"
)

const secretLine = "管道穿刺-紫色长颈鹿-7781"

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

type world struct {
	hub      *Hub
	hubURL   string
	logs     *lockedBuf
	entrySes string // the phone's account session
	devSes   string // the Mini's own session on the same account
	device   *Device
}

// newWorld runs the Account API, the hub and a device data root with one bot
// whose transcript holds secretLine. The entry is a second data root's worth
// of nothing: it only has an account session.
func newWorld(t *testing.T) *world {
	t.Helper()
	ctx := context.Background()
	st, err := account.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	accSrv := httptest.NewServer(nil)
	t.Cleanup(accSrv.Close)
	accSrv.Config.Handler = account.New(st, account.OutboxMailer{Dir: t.TempDir()}, account.Config{BaseURL: accSrv.URL}).Handler()
	acc := account.NewRemote(accSrv.URL)
	phone, err := acc.Register(ctx, "owner@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	mini, err := acc.Register(ctx, "owner@example.com", "correct horse battery") // signs in again
	if err != nil {
		t.Fatal(err)
	}
	if err := acc.Report(ctx, mini.Session, "mini-1", "Mini", []string{"ada"}); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	ui := &web.Server{Root: root, Owner: "u1"}
	if _, _, err := ui.EnsureToken(); err != nil { // serve does this at startup
		t.Fatal(err)
	}
	b, err := bot.Create(root, "ada", "persona", []string{link.NameWeb})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"你好", secretLine} {
		if err := b.Append(bot.Entry{Time: time.Now(), Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(text)}}); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	ui.Register(mux)

	w := &world{logs: &lockedBuf{}, entrySes: phone.Session, devSes: mini.Session}
	w.hub = &Hub{Accounts: acc, Log: log.New(w.logs, "", 0)}
	hubSrv := httptest.NewServer(w.hub.Handler())
	t.Cleanup(hubSrv.Close)
	w.hubURL = hubSrv.URL
	w.hub.Public = hubSrv.URL
	w.device = &Device{Hub: hubSrv.URL, Session: mini.Session, DeviceID: "mini-1", Handler: mux, Authorize: ui.AuthorizeOwner, Log: log.New(w.logs, "", 0)}
	return w
}

// startDevice runs the device's standby loop and waits until the hub has it.
func (w *world) startDevice(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = w.device.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		w.hub.mu.Lock()
		n := len(w.hub.standby)
		w.hub.mu.Unlock()
		if n == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("device never reached standby")
}

func status(err error) int {
	var he *HTTPError
	if errors.As(err, &he) {
		return he.Status
	}
	return 0
}

// Batch 1 pass condition: entry and device meet through the hub, set up an
// end-to-end session in under 2 s, read one transcript page in under 1 s,
// and the hub's log never holds the transcript (or the ticket secret).
func TestPipeReadsTranscriptEndToEnd(t *testing.T) {
	w := newWorld(t)
	w.startDevice(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	tk, err := NewTicket(ctx, w.hubURL, w.entrySes, "mini-1")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := Dial(ctx, tk.Hub, tk)
	if err != nil {
		t.Fatal(err)
	}
	setup := time.Since(start)

	start = time.Now()
	code, body, err := conn.Get(ctx, "/api/bots/ada/transcript")
	page := time.Since(start)
	if err != nil || code != 200 || !strings.Contains(string(body), secretLine) {
		t.Fatalf("transcript: %d %v %.200s", code, err, body)
	}
	t.Logf("setup %s, one page %s (%d bytes)", setup, page, len(body))
	if setup > 2*time.Second || page > time.Second {
		t.Fatalf("too slow: setup %s, page %s", setup, page)
	}
	// Only GET /api/ goes through.
	if code, _, err := conn.Get(ctx, "/healthz"); err != nil || code != 403 {
		t.Fatalf("non-api path: %d %v", code, err)
	}
	if code, _, err := conn.Get(ctx, "/api/bots/nobody/transcript"); err != nil || code != 404 {
		t.Fatalf("unknown bot: %d %v", code, err)
	}

	conn.Close()
	waitFor(t, func() bool { return strings.Contains(w.logs.String(), "closed") })
	logs := w.logs.String()
	for _, leak := range []string{secretLine, "紫色长颈鹿", tk.Secret, w.entrySes, w.devSes} {
		if strings.Contains(logs, leak) {
			t.Fatalf("hub log leaks %q:\n%s", leak, logs)
		}
	}
	if !strings.Contains(logs, "paired") {
		t.Fatalf("log: %s", logs)
	}
	// Hanging up voids the ticket.
	if _, err := Dial(ctx, w.hubURL, tk); err == nil || !strings.Contains(err.Error(), "bad ticket") {
		t.Fatalf("reused ticket: %v", err)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// What the hub relays is ciphertext: a tap on the relay never sees the line.
func TestPipeRelayIsOpaque(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Play the device by hand: a standby socket, then the session socket,
	// sniffing what the hub hands over.
	sb, err := dialWS(ctx, wsURL(w.hubURL), 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	defer sb.Close()
	if err := sendHello(sb, hello{Role: "standby", Session: w.devSes, DeviceID: "mini-1"}); err != nil {
		t.Fatal(err)
	}
	if m, err := readControl(sb); err != nil || m.T != "standby" {
		t.Fatalf("standby: %v %v", m, err)
	}
	tk, err := NewTicket(ctx, w.hubURL, w.entrySes, "mini-1")
	if err != nil {
		t.Fatal(err)
	}
	if m, err := readControl(sb); err != nil || m.T != "open" || m.TicketID != tk.ID || m.Secret != tk.Secret {
		t.Fatalf("open: %v %v", m, err)
	}
	c, err := dialWS(ctx, wsURL(w.hubURL), MaxFrame)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := sendHello(c, hello{Role: "device", TicketID: tk.ID, Secret: tk.Secret}); err != nil {
		t.Fatal(err)
	}
	entryDone := make(chan error, 1)
	go func() {
		conn, err := Dial(ctx, w.hubURL, tk)
		if err == nil {
			_, _, err = conn.Get(ctx, "/api/bots/ada/transcript?q="+secretLine)
			conn.Close()
		}
		entryDone <- err
	}()
	if m, err := readControl(c); err != nil || m.T != "ready" {
		t.Fatalf("ready: %v %v", m, err)
	}
	s, err := handshake(c, tk.Secret, false)
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := c.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(secretLine)) || bytes.Contains(raw, []byte("transcript")) {
		t.Fatal("relayed frame carries plaintext")
	}
	var req frame
	if err := s.open(raw, &req); err != nil || !strings.Contains(req.Path, "transcript") {
		t.Fatalf("open: %v %+v", err, req)
	}
	// A replayed frame does not open (the counter moved on).
	if s.open(raw, &req) == nil {
		t.Fatal("replayed frame opened")
	}
	c.Close()
	if err := <-entryDone; err == nil {
		t.Fatal("entry did not see the device hang up")
	}
}

func TestPipeTicketChecks(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	if _, err := NewTicket(ctx, w.hubURL, "nope", "mini-1"); status(err) != 401 {
		t.Fatalf("bad session: %v", err)
	}
	if _, err := NewTicket(ctx, w.hubURL, w.entrySes, "someone-else"); status(err) != 404 {
		t.Fatalf("foreign device: %v", err)
	}
	if _, err := NewTicket(ctx, w.hubURL, w.entrySes, "mini-1"); status(err) != 409 {
		t.Fatalf("device offline: %v", err)
	}
	w.startDevice(t)
	tk, err := NewTicket(ctx, w.hubURL, w.entrySes, "mini-1")
	if err != nil {
		t.Fatal(err)
	}
	if tk.Expires.After(time.Now().Add(TicketTTL)) || len(tk.Secret) < 40 {
		t.Fatalf("ticket %+v", tk)
	}
	wrong := tk
	wrong.Secret = strings.Repeat("A", len(tk.Secret))
	if _, err := Dial(ctx, w.hubURL, wrong); err == nil || !strings.Contains(err.Error(), "bad ticket") {
		t.Fatalf("wrong secret: %v", err)
	}
	// A browser origin the hub does not list cannot open the socket.
	req, _ := http.NewRequest(http.MethodGet, w.hubURL+"/pipe/v1/ws", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatalf("origin: %d", res.StatusCode)
	}
}

// An unpaired ticket lapses: the waiting side is told and the ticket is gone.
func TestPipeTicketExpiresUnpaired(t *testing.T) {
	w := newWorld(t)
	w.startDevice(t)
	now := time.Now()
	var mu sync.Mutex
	w.hub.Now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w.device.Handler = http.NotFoundHandler()
	w.hub.mu.Lock()
	standby := w.hub.standby
	w.hub.mu.Unlock()
	if len(standby) != 1 {
		t.Fatal("no standby")
	}
	tk, err := NewTicket(ctx, w.hubURL, w.entrySes, "mini-1")
	if err != nil {
		t.Fatal(err)
	}
	// The device took its seat at once; once the ticket lapses the hub drops
	// that seat and the entry cannot join.
	mu.Lock()
	now = now.Add(TicketTTL + time.Second)
	mu.Unlock()
	if _, err := Dial(ctx, w.hubURL, tk); err == nil {
		t.Fatal("expired ticket dialed")
	}
}

func TestWSRejectsOversizedMessage(t *testing.T) {
	srvDone := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrade(w, r, 1024)
		if err != nil {
			srvDone <- err
			return
		}
		defer c.Close()
		_, _, err = c.ReadMessage()
		srvDone <- err
	}))
	defer srv.Close()
	c, err := dialWS(context.Background(), srv.URL, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.WriteMessage(opBinary, make([]byte, 2048)); err != nil {
		t.Fatal(err)
	}
	if err := <-srvDone; !errors.Is(err, errTooBig) {
		t.Fatalf("oversized: %v", err)
	}
}

// review1 P2: Dial and Get honour cancellation, not only deadlines. A
// cancelled request ends the session so a late answer cannot be misread.
func TestPipeContextCancellation(t *testing.T) {
	w := newWorld(t)
	owner := w.device.Handler
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	w.device.Handler = http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/slow" {
			select {
			case <-release:
			case <-r.Context().Done():
			}
			return
		}
		owner.ServeHTTP(rw, r)
	})
	w.startDevice(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	gone, stop := context.WithCancel(ctx)
	stop()
	tk, err := NewTicket(ctx, w.hubURL, w.entrySes, "mini-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Dial(gone, w.hubURL, tk); !errors.Is(err, context.Canceled) {
		t.Fatalf("dial with cancelled ctx: %v", err)
	}
	tk, err = NewTicket(ctx, w.hubURL, w.entrySes, "mini-1")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := Dial(ctx, w.hubURL, tk)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Already cancelled: nothing is sent and the session stays usable.
	if _, _, err := conn.Get(gone, "/api/bots/ada/transcript"); !errors.Is(err, context.Canceled) {
		t.Fatalf("get with cancelled ctx: %v", err)
	}
	if code, _, err := conn.Get(ctx, "/api/bots/ada/transcript"); err != nil || code != 200 {
		t.Fatalf("after a cancelled get: %d %v", code, err)
	}
	// Cancelled while the device is still working: Get returns at once and
	// the session is over.
	mid, cancelMid := context.WithCancel(ctx)
	time.AfterFunc(50*time.Millisecond, cancelMid)
	start := time.Now()
	if _, _, err := conn.Get(mid, "/api/slow"); !errors.Is(err, context.Canceled) {
		t.Fatalf("get cancelled midway: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("cancelled get took %s", d)
	}
	if _, _, err := conn.Get(ctx, "/api/bots/ada/transcript"); err == nil || !strings.Contains(err.Error(), "session ended") {
		t.Fatalf("get after a cancelled one: %v", err)
	}
}

// Dial waiting for a device that never joins gives up when ctx is cancelled.
func TestPipeDialCancelledWhileWaiting(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sb, err := dialWS(ctx, wsURL(w.hubURL), 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	defer sb.Close()
	if err := sendHello(sb, hello{Role: "standby", Session: w.devSes, DeviceID: "mini-1"}); err != nil {
		t.Fatal(err)
	}
	if m, err := readControl(sb); err != nil || m.T != "standby" {
		t.Fatalf("standby: %v %v", m, err)
	}
	tk, err := NewTicket(ctx, w.hubURL, w.entrySes, "mini-1") // the device ignores it
	if err != nil {
		t.Fatal(err)
	}
	mid, cancelMid := context.WithCancel(ctx)
	time.AfterFunc(50*time.Millisecond, cancelMid)
	start := time.Now()
	if _, err := Dial(mid, w.hubURL, tk); !errors.Is(err, context.Canceled) {
		t.Fatalf("dial cancelled while waiting: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("cancelled dial took %s", d)
	}
}

// A caller deadline shorter than the default write bound wins.
func TestWSWriteDeadlineHonoursCaller(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	c := &wsConn{c: a, client: true, max: 1 << 20}
	c.setDeadline(time.Now().Add(50 * time.Millisecond))
	start := time.Now()
	if err := c.WriteMessage(opBinary, []byte("x")); err == nil { // nobody reads b
		t.Fatal("write did not time out")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("write waited %s", d)
	}
}
