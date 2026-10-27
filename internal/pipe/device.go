package pipe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"xbot/internal/pipe/limits"
)

// MaxBody bounds each request and response body so its sealed, base64'd frame stays under MaxFrame.
const MaxBody = limits.MaxBody

// Device answers bounded owner API requests for one signed-in device.
type Device struct {
	Hub      string // hub base URL, such as https://pipe.babelgo.cn:8443
	Session  string // this device's account session
	DeviceID string // the id this device reports to the account
	// Handler is the owner API (the same mux xbot serve runs), and Authorize
	// adds the owner's credential to each request before it runs.
	Handler   http.Handler
	Authorize func(*http.Request) error
	// Hooks answers webhooks the hub forwards from its public ingress. It
	// runs without the owner's credential; nil refuses every hook.
	Hooks http.Handler
	// Standby, when set, hears true once the hub acknowledges this device's
	// standby and false when that standby ends.
	Standby func(up bool)
	Log     *log.Logger
}

func (d *Device) logf(format string, args ...any) {
	if d.Log != nil {
		d.Log.Printf(format, args...)
	}
}

func wsURL(hub string) string { return strings.TrimRight(hub, "/") + "/pipe/v1/ws" }

func sendHello(c *wsConn, h hello) error {
	raw, err := json.Marshal(h)
	if err != nil {
		return err
	}
	return c.WriteMessage(opText, raw)
}

func readControl(c *wsConn) (control, error) {
	op, raw, err := c.ReadMessage()
	if err != nil {
		return control{}, err
	}
	var m control
	if op != opText || json.Unmarshal(raw, &m) != nil {
		return control{}, errors.New("pipe: unexpected message from hub")
	}
	if m.T == "error" {
		return m, fmt.Errorf("pipe: hub refused: %s", m.Error)
	}
	return m, nil
}

// Run keeps a standby socket open and serves each session the hub opens,
// reconnecting with backoff, until ctx ends.
func (d *Device) Run(ctx context.Context) error {
	backoff := time.Second
	for ctx.Err() == nil {
		start := time.Now()
		err := d.standby(ctx)
		if ctx.Err() != nil {
			break
		}
		if time.Since(start) > time.Minute {
			backoff = time.Second
		}
		d.logf("pipe: standby ended: %v; retrying in %s", err, backoff)
		select {
		case <-ctx.Done():
		case <-time.After(backoff):
		}
		backoff = min(2*backoff, time.Minute)
	}
	return ctx.Err()
}

func (d *Device) standby(ctx context.Context) error {
	ctx, cancelSessions := context.WithCancel(ctx)
	var sessions sync.WaitGroup
	defer func() { cancelSessions(); sessions.Wait() }()
	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	c, err := dialWS(dctx, wsURL(d.Hub), 64<<10)
	cancel()
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	defer c.Close()
	if err := sendHello(c, hello{Role: "standby", Session: d.Session, DeviceID: d.DeviceID}); err != nil {
		return err
	}
	if m, err := readControl(c); err != nil || m.T != "standby" {
		return cmpErr(err, "pipe: no standby ack")
	}
	if d.Standby != nil {
		d.Standby(true)
		defer d.Standby(false)
	}
	for {
		m, err := readControl(c)
		if err != nil {
			return err
		}
		if m.T == "open" && m.TicketID != "" && m.Secret != "" {
			sessions.Add(1)
			go func() {
				defer sessions.Done()
				if err := d.Serve(ctx, m.TicketID, m.Secret); err != nil && ctx.Err() == nil {
					d.logf("pipe: session %s: %v", short(m.TicketID), err)
				}
			}()
		}
		if m.T == "hook" && m.ID != "" {
			sessions.Add(1)
			go func() {
				defer sessions.Done()
				d.hook(ctx, c, m)
			}()
		}
	}
}

func cmpErr(err error, msg string) error {
	if err != nil {
		return err
	}
	return errors.New(msg)
}

// Serve joins one ticket's session as the device and answers requests until
// either side hangs up.
func (d *Device) Serve(ctx context.Context, ticketID, secret string) error {
	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	c, err := dialWS(dctx, wsURL(d.Hub), MaxFrame)
	cancel()
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	defer c.Close()
	if err := sendHello(c, hello{Role: "device", TicketID: ticketID, Secret: secret}); err != nil {
		return err
	}
	if m, err := readControl(c); err != nil || m.T != "ready" {
		return cmpErr(err, "pipe: no ready")
	}
	s, err := handshake(c, secret, false)
	if err != nil {
		return err
	}
	for {
		op, msg, err := c.ReadMessage()
		if err != nil {
			return nil // the entry or hub hung up
		}
		var req frame
		if op != opBinary || s.open(msg, &req) != nil || req.T != "req" {
			return errors.New("pipe: bad frame")
		}
		res := d.answer(ctx, req)
		out, err := s.seal(res)
		if err != nil {
			return err
		}
		if err := c.WriteMessage(opBinary, out); err != nil {
			return err
		}
	}
}

func (d *Device) answer(ctx context.Context, req frame) frame {
	res := frame{T: "res", ID: req.ID}
	fail := func(status int, msg string) frame {
		res.CT = "application/json"
		res.Status, res.Body = status, []byte(`{"error":"`+msg+`"}`)
		return res
	}
	u, err := url.ParseRequestURI(req.Path)
	allowed := req.Method == "GET" || req.Method == "POST" || req.Method == "PATCH" || req.Method == "PUT" || req.Method == "DELETE"
	if !allowed || err != nil || u.Host != "" || u.Scheme != "" || !strings.HasPrefix(req.Path, "/api/") || !strings.HasPrefix(u.Path, "/api/") || strings.Contains(u.Path, "\\") {
		return fail(http.StatusForbidden, "pipe serves only owner /api/ methods")
	}
	if len(req.Body) > MaxBody {
		return fail(http.StatusRequestEntityTooLarge, "request too large for the pipe")
	}
	if len(req.CT) > 256 || strings.ContainsAny(req.CT, "\r\n") {
		return fail(http.StatusBadRequest, "bad content type")
	}
	if len(req.INM) > 256 || strings.ContainsAny(req.INM, "\r\n") {
		return fail(http.StatusBadRequest, "bad etag")
	}
	r, err := http.NewRequestWithContext(ctx, req.Method, u.RequestURI(), bytes.NewReader(req.Body))
	if err != nil {
		return fail(http.StatusBadRequest, "bad path")
	}
	r.Header.Set("Content-Type", req.CT)
	r.Header.Set("If-None-Match", req.INM)
	r.Host, r.RemoteAddr = "pipe.local", "pipe"
	if d.Authorize != nil {
		if err := d.Authorize(r); err != nil {
			return fail(http.StatusInternalServerError, "owner credential unavailable")
		}
	}
	w := &capture{header: http.Header{}}
	d.Handler.ServeHTTP(w, r)
	if w.over {
		return fail(http.StatusRequestEntityTooLarge, "response too large for the pipe")
	}
	res.Status, res.Body, res.CT = cmpStatus(w.status), w.body.Bytes(), w.header.Get("Content-Type")
	// Only static metadata crosses the wire; never cookies or arbitrary headers.
	if strings.HasPrefix(u.Path, "/api/ui/static/") {
		res.ETag, res.Tip, res.Cache = w.header.Get("ETag"), w.header.Get("X-Xbot-Tip"), w.header.Get("Cache-Control")
	}
	return res
}

func cmpStatus(s int) int {
	if s == 0 {
		return http.StatusOK
	}
	return s
}

// capture is a ResponseWriter that keeps at most MaxBody bytes.
type capture struct {
	header http.Header
	status int
	body   bytes.Buffer
	over   bool
}

func (c *capture) Header() http.Header { return c.header }
func (c *capture) WriteHeader(s int) {
	if c.status == 0 {
		c.status = s
	}
}
func (c *capture) Write(p []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	if c.body.Len()+len(p) > MaxBody {
		c.over = true
		return 0, errors.New("pipe: response too large")
	}
	return c.body.Write(p)
}
