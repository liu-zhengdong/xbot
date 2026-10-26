package pipe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// NewTicket asks the hub for a ticket to deviceID with an account session.
func NewTicket(ctx context.Context, hub, session, deviceID string) (TicketOut, error) {
	body, _ := json.Marshal(map[string]string{"device_id": deviceID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(hub, "/")+"/pipe/v1/ticket", bytes.NewReader(body))
	if err != nil {
		return TicketOut{}, err
	}
	req.Header.Set("Authorization", "Bearer "+session)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return TicketOut{}, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if err != nil {
		return TicketOut{}, err
	}
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		return TicketOut{}, &HTTPError{Status: res.StatusCode, Message: e.Error}
	}
	var out TicketOut
	return out, json.Unmarshal(raw, &out)
}

// HTTPError is a hub refusal.
type HTTPError struct {
	Status  int
	Message string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("pipe: %d %s", e.Status, e.Message) }

// Conn is the entry's end of one session. Requests go one at a time; a
// failed or cancelled request ends the session, so a late answer can never
// be read as the next request's.
type Conn struct {
	ws     *wsConn
	s      *sealer
	mu     sync.Mutex
	n      int
	broken error
}

// withContext bounds c by ctx's deadline and closes it if ctx is cancelled
// first. done reports whether the cancellation fired, and clears the bound.
func withContext(ctx context.Context, c *wsConn) (done func() bool) {
	dl, _ := ctx.Deadline()
	c.setDeadline(dl)
	stop := context.AfterFunc(ctx, func() { _ = c.c.Close() })
	return func() bool {
		fired := !stop()
		c.setDeadline(time.Time{})
		return fired
	}
}

// Dial joins the ticket's session as the entry and completes the handshake
// once the device is there.
func Dial(ctx context.Context, hub string, t TicketOut) (*Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c, err := dialWS(ctx, wsURL(hub), MaxFrame)
	if err != nil {
		return nil, err
	}
	done := withContext(ctx, c)
	s, err := func() (*sealer, error) {
		if err := sendHello(c, hello{Role: "entry", TicketID: t.ID, Secret: t.Secret}); err != nil {
			return nil, err
		}
		if m, err := readControl(c); err != nil || m.T != "ready" {
			return nil, cmpErr(err, "pipe: no ready")
		}
		return handshake(c, t.Secret, true)
	}()
	if done() && err == nil {
		err = ctx.Err()
	}
	if err != nil {
		c.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return &Conn{ws: c, s: s}, nil
}

// Get runs GET path (such as /api/bots/ada/transcript) on the device's owner
// API and returns its status and body.
func (c *Conn) Get(ctx context.Context, path string) (int, []byte, error) {
	status, body, _, err := c.Do(ctx, http.MethodGet, path, nil, "")
	return status, body, err
}

// Do runs one bounded owner API request. A failed or cancelled exchange closes
// the connection; callers may obtain a fresh ticket, but must not replay writes.
func (c *Conn) Do(ctx context.Context, method, path string, body []byte, contentType string) (int, []byte, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.broken != nil {
		return 0, nil, "", c.broken
	}
	if err := ctx.Err(); err != nil {
		return 0, nil, "", err
	}
	if len(body) > MaxBody {
		return 0, nil, "", errors.New("pipe: request too large")
	}
	done := withContext(ctx, c.ws)
	res, err := c.roundTrip(method, path, body, contentType)
	if done() && err == nil {
		err = ctx.Err()
	}
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		c.broken = fmt.Errorf("pipe: session ended: %w", err)
		c.ws.Close()
		return 0, nil, "", err
	}
	return res.Status, res.Body, res.CT, nil
}

func (c *Conn) roundTrip(method, path string, body []byte, contentType string) (frame, error) {
	c.n++
	id := strconv.Itoa(c.n)
	msg, err := c.s.seal(frame{T: "req", ID: id, Method: method, Path: path, Body: body, CT: contentType})
	if err != nil {
		return frame{}, err
	}
	if err := c.ws.WriteMessage(opBinary, msg); err != nil {
		return frame{}, err
	}
	op, raw, err := c.ws.ReadMessage()
	if err != nil {
		return frame{}, err
	}
	var res frame
	if op != opBinary || c.s.open(raw, &res) != nil || res.T != "res" || res.ID != id || len(res.Body) > MaxBody || res.Status < 200 || res.Status > 599 {
		return frame{}, errors.New("pipe: bad frame")
	}
	return res, nil
}

// Close ends the session; the hub then ends the device's side and voids the ticket.
func (c *Conn) Close() error { return c.ws.Close() }
