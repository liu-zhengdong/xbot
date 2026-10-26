package pipe

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// A minimal RFC 6455 WebSocket, both sides, standard library only: text and
// binary messages, continuation frames, ping/pong and close. No extensions.

const (
	opCont   = 0x0
	opText   = 0x1
	opBinary = 0x2
	opClose  = 0x8
	opPing   = 0x9
	opPong   = 0xA
)

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

var errTooBig = errors.New("pipe: message too large")

type wsConn struct {
	c      net.Conn
	r      *bufio.Reader
	client bool // clients mask what they send
	max    int
	wmu    sync.Mutex
	closed bool
}

func acceptKey(key string) string {
	sum := sha1.Sum([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// upgrade answers a WebSocket handshake by hijacking the connection.
func upgrade(w http.ResponseWriter, r *http.Request, max int) (*wsConn, error) {
	if !headerHas(r.Header, "Connection", "upgrade") || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") ||
		r.Header.Get("Sec-WebSocket-Version") != "13" || r.Header.Get("Sec-WebSocket-Key") == "" {
		http.Error(w, "websocket upgrade required", http.StatusBadRequest)
		return nil, errors.New("pipe: not a websocket request")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "no hijack", http.StatusInternalServerError)
		return nil, errors.New("pipe: response cannot hijack")
	}
	c, rw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	_ = c.SetDeadline(time.Time{})
	fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", acceptKey(r.Header.Get("Sec-WebSocket-Key")))
	if err := rw.Flush(); err != nil {
		c.Close()
		return nil, err
	}
	return &wsConn{c: c, r: rw.Reader, max: max}, nil
}

func headerHas(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}

// dialWS opens a client WebSocket to an http(s) or ws(s) URL.
func dialWS(ctx context.Context, raw string, max int) (*wsConn, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	secure := u.Scheme == "https" || u.Scheme == "wss"
	if !secure && u.Scheme != "http" && u.Scheme != "ws" {
		return nil, fmt.Errorf("pipe: bad hub scheme %q", u.Scheme)
	}
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), map[bool]string{true: "443", false: "80"}[secure])
	}
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, err
	}
	if secure {
		tc := tls.Client(c, &tls.Config{ServerName: u.Hostname(), NextProtos: []string{"http/1.1"}})
		if err := tc.HandshakeContext(ctx); err != nil {
			c.Close()
			return nil, err
		}
		c = tc
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = c.SetDeadline(dl)
	}
	var key [16]byte
	_, _ = rand.Read(key[:])
	k := base64.StdEncoding.EncodeToString(key[:])
	path := u.RequestURI()
	fmt.Fprintf(c, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", path, u.Host, k)
	br := bufio.NewReader(c)
	res, err := http.ReadResponse(br, &http.Request{Method: http.MethodGet})
	if err != nil {
		c.Close()
		return nil, err
	}
	if res.StatusCode != http.StatusSwitchingProtocols || res.Header.Get("Sec-WebSocket-Accept") != acceptKey(k) {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		res.Body.Close()
		c.Close()
		return nil, fmt.Errorf("pipe: websocket handshake %d %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	_ = c.SetDeadline(time.Time{})
	return &wsConn{c: c, r: br, client: true, max: max}, nil
}

func (w *wsConn) writeFrame(op byte, payload []byte) error {
	w.wmu.Lock()
	defer w.wmu.Unlock()
	if w.closed {
		return net.ErrClosed
	}
	hdr := make([]byte, 2, 14)
	hdr[0] = 0x80 | op
	switch n := len(payload); {
	case n < 126:
		hdr[1] = byte(n)
	case n <= 0xFFFF:
		hdr[1] = 126
		hdr = binary.BigEndian.AppendUint16(hdr, uint16(n))
	default:
		hdr[1] = 127
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	if w.client {
		hdr[1] |= 0x80
		var mask [4]byte
		_, _ = rand.Read(mask[:])
		hdr = append(hdr, mask[:]...)
		masked := make([]byte, len(payload))
		for i, b := range payload {
			masked[i] = b ^ mask[i%4]
		}
		payload = masked
	}
	_ = w.c.SetWriteDeadline(time.Now().Add(30 * time.Second))
	if _, err := w.c.Write(append(hdr, payload...)); err != nil {
		return err
	}
	if op == opClose {
		w.closed = true
	}
	return nil
}

// WriteMessage sends one text or binary message.
func (w *wsConn) WriteMessage(op byte, payload []byte) error { return w.writeFrame(op, payload) }

// ReadMessage returns the next text or binary message, answering pings and
// rejecting anything over max bytes.
func (w *wsConn) ReadMessage() (byte, []byte, error) {
	var op byte
	var msg []byte
	for {
		var h [2]byte
		if _, err := io.ReadFull(w.r, h[:]); err != nil {
			return 0, nil, err
		}
		fin, code, masked := h[0]&0x80 != 0, h[0]&0x0F, h[1]&0x80 != 0
		if h[0]&0x70 != 0 || masked == w.client {
			return 0, nil, errors.New("pipe: bad websocket frame")
		}
		n := uint64(h[1] & 0x7F)
		switch n {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(w.r, b[:]); err != nil {
				return 0, nil, err
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(w.r, b[:]); err != nil {
				return 0, nil, err
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		if n > uint64(w.max) || uint64(len(msg))+n > uint64(w.max) {
			_ = w.writeFrame(opClose, closeBody(1009))
			return 0, nil, errTooBig
		}
		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(w.r, mask[:]); err != nil {
				return 0, nil, err
			}
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(w.r, payload); err != nil {
			return 0, nil, err
		}
		if masked {
			for i := range payload {
				payload[i] ^= mask[i%4]
			}
		}
		switch code {
		case opPing:
			if n > 125 {
				return 0, nil, errors.New("pipe: bad websocket frame")
			}
			if err := w.writeFrame(opPong, payload); err != nil {
				return 0, nil, err
			}
			continue
		case opPong:
			continue
		case opClose:
			_ = w.writeFrame(opClose, nil)
			return 0, nil, io.EOF
		case opText, opBinary:
			if msg != nil {
				return 0, nil, errors.New("pipe: bad websocket frame")
			}
			op = code
		case opCont:
			if msg == nil && op == 0 {
				return 0, nil, errors.New("pipe: bad websocket frame")
			}
		default:
			return 0, nil, errors.New("pipe: bad websocket frame")
		}
		msg = append(msg, payload...)
		if msg == nil {
			msg = []byte{}
		}
		if fin {
			return op, msg, nil
		}
	}
}

func closeBody(code uint16) []byte { return binary.BigEndian.AppendUint16(nil, code) }

// Close sends a close frame (best effort) and drops the connection.
func (w *wsConn) Close() error {
	_ = w.writeFrame(opClose, closeBody(1000))
	return w.c.Close()
}
