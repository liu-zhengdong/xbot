package browser

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Conn is a CDP session over one websocket.
type Conn struct {
	c  net.Conn
	r  *bufio.Reader
	w  *bufio.Writer
	id int
}

// Dial opens the debugger websocket. The host must be loopback.
func Dial(ctx context.Context, wsURL string) (*Conn, error) {
	u, err := url.Parse(wsURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "ws" {
		return nil, errors.New("browser: debugger url must be ws")
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, errors.New("browser: debugger url is not loopback")
	}
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return nil, err
	}
	c := &Conn{c: conn, r: bufio.NewReader(conn), w: bufio.NewWriter(conn)}
	if err := c.handshake(u); err != nil {
		conn.Close()
		return nil, err
	}
	return c, nil
}

// Close closes the socket.
func (c *Conn) Close() error {
	if c == nil || c.c == nil {
		return nil
	}
	return c.c.Close()
}

// Call sends one CDP command and returns its result.
func (c *Conn) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if c == nil {
		return nil, errors.New("browser: not open")
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(10 * time.Second)
	}
	_ = c.c.SetDeadline(deadline)
	c.id++
	id := c.id
	msg := map[string]any{"id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	if err := writeText(c.w, body); err != nil {
		return nil, err
	}
	if err := c.w.Flush(); err != nil {
		return nil, err
	}
	for {
		opcode, payload, err := readFrame(c.r)
		if err != nil {
			return nil, err
		}
		if opcode == 8 {
			return nil, errors.New("browser: debugger closed")
		}
		if opcode != 1 {
			continue
		}
		var env struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(payload, &env); err != nil {
			continue
		}
		if env.ID != id {
			continue
		}
		if env.Error.Message != "" {
			return nil, fmt.Errorf("browser: %s", env.Error.Message)
		}
		return env.Result, nil
	}
}

func (c *Conn) handshake(u *url.URL) error {
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		return err
	}
	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	fmt.Fprintf(c.w, "GET %s HTTP/1.1\r\n", path)
	fmt.Fprintf(c.w, "Host: %s\r\n", u.Host)
	io.WriteString(c.w, "Upgrade: websocket\r\nConnection: Upgrade\r\n")
	fmt.Fprintf(c.w, "Sec-WebSocket-Key: %s\r\n", base64.StdEncoding.EncodeToString(key[:]))
	io.WriteString(c.w, "Sec-WebSocket-Version: 13\r\n\r\n")
	if err := c.w.Flush(); err != nil {
		return err
	}
	resp, err := http.ReadResponse(c.r, &http.Request{Method: http.MethodGet})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return fmt.Errorf("browser: websocket status %s", resp.Status)
	}
	return nil
}

func writeText(w io.Writer, payload []byte) error {
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	hdr := []byte{0x81}
	n := len(payload)
	switch {
	case n < 126:
		hdr = append(hdr, byte(0x80|n))
	case n <= 65535:
		hdr = append(hdr, 0x80|126, byte(n>>8), byte(n))
	default:
		return errors.New("browser: frame too large")
	}
	hdr = append(hdr, mask[:]...)
	masked := make([]byte, n)
	for i := range payload {
		masked[i] = payload[i] ^ mask[i%4]
	}
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	_, err := w.Write(masked)
	return err
}

func readFrame(r io.Reader) (byte, []byte, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	opcode := hdr[0] & 0x0f
	masked := hdr[1]&0x80 != 0
	n := int(hdr[1] & 0x7f)
	switch n {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return 0, nil, err
		}
		n = int(binary.BigEndian.Uint16(ext[:]))
	case 127:
		return 0, nil, errors.New("browser: frame too large")
	}
	if n > 1<<20 {
		return 0, nil, errors.New("browser: frame too large")
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(r, mask[:]); err != nil {
			return 0, nil, err
		}
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return opcode, payload, nil
}

func loopbackHost(host string) bool {
	host = strings.TrimSpace(host)
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
