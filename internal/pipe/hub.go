// Package pipe is the owner's remote-console pipe (ADR 0024 B, batch 1): an
// account session buys a short ticket for one of the account's devices, the
// entry (phone, remote desktop) and the device both dial the hub's WebSocket
// with it, and the hub relays opaque binary frames between them. Entry and
// device agree an end-to-end key the hub never sees (crypto.go) and run the
// device's owner /api over it (device.go, entry.go).
package pipe

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"xbot/internal/account"
)

// Limits of the hub.
const (
	TicketTTL        = 5 * time.Minute  // a ticket must be paired within this
	SessionIdle      = 10 * time.Minute // a paired session with no frames ends
	MaxFrame         = 1 << 20          // one relayed message, ciphertext included
	MaxTicketsPerAcc = 8
	MaxTickets       = 1024
	MaxStandby       = 1024
	authWait         = 10 * time.Second
	pingEvery        = 30 * time.Second
)

// Accounts is the part of the Account API the hub needs; account.Remote has it.
type Accounts interface {
	Me(ctx context.Context, session string) (account.Me, error)
	Devices(ctx context.Context, session string) ([]account.DeviceView, error)
}

// Hub issues tickets and relays paired WebSockets. It keeps tickets in memory:
// a restart ends every session, and clients ask for a new ticket.
type Hub struct {
	Accounts Accounts
	// Public is the hub's public base URL, returned with each ticket.
	Public string
	// Origins lists browser origins allowed to open the WebSocket; requests
	// without an Origin header (non-browser clients) are always allowed.
	Origins []string
	Log     *log.Logger
	Now     func() time.Time

	mu       sync.Mutex
	tickets  map[string]*ticket
	standby  map[string]*wsConn // account key + "/" + device id
	hooks    map[string]pendingHook
	hookLoad map[string]int // in-flight hooks per standby slot
}

type ticket struct {
	id, account, device string
	hash                [32]byte
	expires             time.Time
	entry, dev          *wsConn
	paired              bool
	done                chan struct{} // closed when the relay ends
	// The first side to arrive is read while it waits, so its hang-up voids
	// the ticket at once; stopped closes when that read returns.
	stopped chan struct{}
	waitErr error
}

// hello is the first (text) message on every WebSocket.
type hello struct {
	Role     string `json:"role"` // standby | entry | device
	TicketID string `json:"ticket_id,omitempty"`
	Secret   string `json:"secret,omitempty"`
	Session  string `json:"session,omitempty"`
	DeviceID string `json:"device_id,omitempty"`
}

// control is a hub-to-client text message.
type control struct {
	T        string `json:"t"` // standby | open | ready | error
	TicketID string `json:"ticket_id,omitempty"`
	Secret   string `json:"secret,omitempty"`
	Error    string `json:"error,omitempty"`
	// A forwarded webhook (hook) and the device's answer (hook_res).
	ID     string            `json:"id,omitempty"`
	Path   string            `json:"path,omitempty"`
	Header map[string]string `json:"header,omitempty"`
	Body   []byte            `json:"body,omitempty"`
	Status int               `json:"status,omitempty"`
}

// TicketOut is the POST /pipe/v1/ticket answer. The secret is shown once.
type TicketOut struct {
	ID      string    `json:"ticket_id"`
	Secret  string    `json:"ticket_secret"`
	Expires time.Time `json:"expires"`
	Hub     string    `json:"hub,omitempty"`
}

func (h *Hub) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("POST /pipe/v1/ticket", h.ticketCORS(h.serveTicket))
	mux.HandleFunc("OPTIONS /pipe/v1/ticket", h.ticketCORS(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	mux.HandleFunc("GET /pipe/v1/ws", h.serveWS)
	mux.HandleFunc(hookIngressPattern, h.serveHook)
	return mux
}

// ticketCORS shares the explicit browser allowlist with WebSocket upgrades.
func (h *Hub) ticketCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		if origin := r.Header.Get("Origin"); origin != "" {
			if !slices.Contains(h.Origins, origin) {
				writeErr(w, http.StatusForbidden, "origin not allowed")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		}
		next(w, r)
	}
}

func (h *Hub) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func (h *Hub) logf(format string, args ...any) {
	if h.Log != nil {
		h.Log.Printf(format, args...)
	}
}

// short is what logs show of a ticket id.
func short(id string) string { return id[:min(8, len(id))] }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ownDevice checks the session and that deviceID is one of its account's
// devices; it returns the account key.
func (h *Hub) ownDevice(ctx context.Context, session, deviceID string) (string, int, error) {
	if session == "" || deviceID == "" || len(deviceID) > 128 {
		return "", 400, errors.New("session and device_id required")
	}
	me, err := h.Accounts.Me(ctx, session)
	if err != nil || me.Key == "" {
		return "", 401, errors.New("not signed in")
	}
	devs, err := h.Accounts.Devices(ctx, session)
	if err != nil {
		return "", 401, errors.New("not signed in")
	}
	if !slices.ContainsFunc(devs, func(d account.DeviceView) bool { return d.ID == deviceID }) {
		return "", 404, errors.New("no such device")
	}
	return me.Key, 0, nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (h *Hub) serveTicket(w http.ResponseWriter, r *http.Request) {
	session, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		writeErr(w, 401, "not signed in")
		return
	}
	var in struct {
		DeviceID string `json:"device_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
		writeErr(w, 400, "bad request")
		return
	}
	key, status, err := h.ownDevice(r.Context(), session, in.DeviceID)
	if err != nil {
		writeErr(w, status, err.Error())
		return
	}
	var secret [32]byte
	_, _ = rand.Read(secret[:])
	out := TicketOut{ID: randomHex(16), Secret: base64.RawURLEncoding.EncodeToString(secret[:]), Expires: h.now().Add(TicketTTL), Hub: h.Public}
	t := &ticket{id: out.ID, account: key, device: in.DeviceID, hash: sha256.Sum256([]byte(out.Secret)), expires: out.Expires, done: make(chan struct{})}

	h.mu.Lock()
	h.sweepLocked()
	standby := h.standby[key+"/"+in.DeviceID]
	mine := 0
	for _, o := range h.tickets {
		if o.account == key {
			mine++
		}
	}
	switch {
	case standby == nil:
		h.mu.Unlock()
		writeErr(w, 409, "device not connected")
		return
	case mine >= MaxTicketsPerAcc || len(h.tickets) >= MaxTickets:
		h.mu.Unlock()
		writeErr(w, 429, "too many tickets")
		return
	}
	if h.tickets == nil {
		h.tickets = make(map[string]*ticket)
	}
	h.tickets[t.id] = t
	h.mu.Unlock()

	// The device learns the secret from its own signed-in standby socket; the
	// hub keeps only its hash after this.
	msg, _ := json.Marshal(control{T: "open", TicketID: out.ID, Secret: out.Secret})
	if err := standby.WriteMessage(opText, msg); err != nil {
		h.mu.Lock()
		delete(h.tickets, t.id)
		h.mu.Unlock()
		writeErr(w, 409, "device not connected")
		return
	}
	h.logf("pipe: ticket %s issued", short(t.id))
	writeJSON(w, 200, out)
}

func (h *Hub) sweepLocked() {
	for id, t := range h.tickets {
		if !t.paired && !h.now().Before(t.expires) {
			delete(h.tickets, id)
			for _, c := range []*wsConn{t.entry, t.dev} {
				if c != nil {
					_ = c.c.Close() // its waiter's read ends
				}
			}
		}
	}
}

func (h *Hub) originOK(r *http.Request) bool {
	o := r.Header.Get("Origin")
	return o == "" || slices.Contains(h.Origins, strings.TrimRight(o, "/"))
}

func (h *Hub) serveWS(w http.ResponseWriter, r *http.Request) {
	if !h.originOK(r) {
		writeErr(w, 403, "origin not allowed")
		return
	}
	c, err := upgrade(w, r, MaxFrame)
	if err != nil {
		return
	}
	_ = c.c.SetReadDeadline(time.Now().Add(authWait))
	op, raw, err := c.ReadMessage()
	_ = c.c.SetReadDeadline(time.Time{})
	var in hello
	if err != nil || op != opText || json.Unmarshal(raw, &in) != nil {
		c.Close()
		return
	}
	switch in.Role {
	case "standby":
		h.serveStandby(r.Context(), c, in)
	case "entry", "device":
		h.serveSide(c, in)
	default:
		refuse(c, "bad role")
	}
}

func refuse(c *wsConn, msg string) {
	raw, _ := json.Marshal(control{T: "error", Error: msg})
	_ = c.WriteMessage(opText, raw)
	c.Close()
}

// keepalive pings c until stop closes, so proxies keep idle sockets open.
func keepalive(c *wsConn, stop <-chan struct{}) {
	t := time.NewTicker(pingEvery)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			if c.writeFrame(opPing, nil) != nil {
				return
			}
		}
	}
}

// serveStandby holds a signed-in device's socket so tickets can reach it.
// A newer standby for the same device replaces the older one.
func (h *Hub) serveStandby(ctx context.Context, c *wsConn, in hello) {
	key, _, err := h.ownDevice(ctx, in.Session, in.DeviceID)
	if err != nil {
		refuse(c, err.Error())
		return
	}
	slot := key + "/" + in.DeviceID
	h.mu.Lock()
	old := h.standby[slot]
	if old == nil && len(h.standby) >= MaxStandby {
		h.mu.Unlock()
		refuse(c, "hub full")
		return
	}
	if h.standby == nil {
		h.standby = make(map[string]*wsConn)
	}
	h.standby[slot] = c
	h.mu.Unlock()
	if old != nil {
		old.Close()
	}
	raw, _ := json.Marshal(control{T: "standby"})
	if c.WriteMessage(opText, raw) == nil {
		stop := make(chan struct{})
		go keepalive(c, stop)
		for {
			// Upward, a standby socket carries only webhook answers.
			op, raw, err := c.ReadMessage()
			if err != nil {
				break
			}
			var m control
			if op == opText && json.Unmarshal(raw, &m) == nil && m.T == "hook_res" {
				h.hookAnswer(slot, m)
			}
		}
		close(stop)
	}
	h.mu.Lock()
	if h.standby[slot] == c {
		delete(h.standby, slot)
	}
	h.mu.Unlock()
	c.Close()
}

// serveSide seats an entry or device on its ticket. The second to arrive
// runs the relay; the first waits for it to finish or for the ticket to lapse.
func (h *Hub) serveSide(c *wsConn, in hello) {
	sum := sha256.Sum256([]byte(in.Secret))
	h.mu.Lock()
	h.sweepLocked()
	t := h.tickets[in.TicketID]
	if t == nil || subtle.ConstantTimeCompare(sum[:], t.hash[:]) != 1 || t.paired {
		h.mu.Unlock()
		refuse(c, "bad ticket")
		return
	}
	slot := &t.entry
	if in.Role == "device" {
		slot = &t.dev
	}
	if *slot != nil {
		h.mu.Unlock()
		refuse(c, "bad ticket")
		return
	}
	*slot = c
	if t.entry == nil || t.dev == nil {
		t.stopped = make(chan struct{})
		h.mu.Unlock()
		h.wait(t, c)
		return
	}
	t.paired = true
	first := t.entry
	if in.Role == "entry" {
		first = t.dev
	}
	h.mu.Unlock()
	// Stop the waiter's read before anyone may send: clients speak only after ready.
	_ = first.c.SetReadDeadline(time.Now())
	<-t.stopped
	_ = first.c.SetReadDeadline(time.Time{})
	if !errors.Is(t.waitErr, os.ErrDeadlineExceeded) {
		t.entry.Close() // the first side left (or spoke) before its peer came
		t.dev.Close()
		h.mu.Lock()
		delete(h.tickets, t.id)
		h.mu.Unlock()
		close(t.done)
		return
	}
	h.relay(t)
}

// wait holds the first side until its peer arrives, it hangs up, or the
// ticket lapses.
func (h *Hub) wait(t *ticket, c *wsConn) {
	go func() {
		_, _, err := c.ReadMessage()
		if err == nil {
			err = io.ErrUnexpectedEOF // nothing may come before ready
		}
		t.waitErr = err
		close(t.stopped)
	}()
	timer := time.NewTimer(t.expires.Sub(h.now()))
	defer timer.Stop()
	select {
	case <-t.stopped:
	case <-timer.C:
	}
	h.mu.Lock()
	paired := t.paired
	if !paired && h.tickets[t.id] == t {
		delete(h.tickets, t.id)
	}
	h.mu.Unlock()
	if paired {
		<-t.done
		return
	}
	h.logf("pipe: ticket %s ended unpaired", short(t.id))
	refuse(c, "ticket ended")
	<-t.stopped
}

// relay copies binary messages both ways until either side ends, then ends
// the other and voids the ticket. It never looks inside a frame.
func (h *Hub) relay(t *ticket) {
	defer close(t.done)
	ready, _ := json.Marshal(control{T: "ready"})
	var up, down atomic.Int64
	if t.entry.WriteMessage(opText, ready) == nil && t.dev.WriteMessage(opText, ready) == nil {
		h.logf("pipe: ticket %s paired", short(t.id))
		stop := make(chan struct{})
		go keepalive(t.entry, stop)
		go keepalive(t.dev, stop)
		errc := make(chan error, 2)
		go func() { errc <- pump(t.entry, t.dev, &up) }()
		go func() { errc <- pump(t.dev, t.entry, &down) }()
		<-errc
		t.entry.Close()
		t.dev.Close()
		<-errc
		close(stop)
	}
	t.entry.Close()
	t.dev.Close()
	h.mu.Lock()
	delete(h.tickets, t.id)
	h.mu.Unlock()
	h.logf("pipe: ticket %s closed, entry->device %d bytes, device->entry %d bytes", short(t.id), up.Load(), down.Load())
}

func pump(from, to *wsConn, n *atomic.Int64) error {
	for {
		_ = from.c.SetReadDeadline(time.Now().Add(SessionIdle))
		op, msg, err := from.ReadMessage()
		if err != nil {
			return err
		}
		if op != opBinary {
			return io.ErrUnexpectedEOF
		}
		n.Add(int64(len(msg)))
		if err := to.WriteMessage(opBinary, msg); err != nil {
			return err
		}
	}
}
