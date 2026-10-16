package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client is a Backend that talks to a vault daemon over a unix socket. It never
// learns any secret value: the daemon injects at egress and redacts responses.
type Client struct {
	bot    string
	socket string

	once sync.Once
	tr   *http.Transport
}

// OpenClient returns a daemon client for one bot. The daemon enforces access by
// SO_PEERCRED; no token is carried.
func OpenClient(socket, bot string) (*Client, error) {
	socket = strings.TrimSpace(socket)
	if socket == "" {
		return nil, errors.New("vault: empty socket path")
	}
	bot = strings.TrimSpace(bot)
	if !botNameRE.MatchString(bot) {
		return nil, errors.New("vault: bad bot name")
	}
	return &Client{bot: bot, socket: socket}, nil
}

// transport lazily builds the one keep-alive unix-socket transport so a
// long-lived client reuses a single persistent connection.
func (c *Client) transport() *http.Transport {
	c.once.Do(func() {
		socket := c.socket
		c.tr = &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socket)
			},
			MaxIdleConns:        4,
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     90 * time.Second,
		}
	})
	return c.tr
}

// Transport makes the Client a RoundTripper wrapper. The base argument is
// ignored: the real outbound request is made by the daemon.
func (c *Client) Transport(http.RoundTripper) http.RoundTripper { return c }

// RoundTrip rewrites an outbound request into a /v1/forward call and rebuilds
// the daemon's reply. The original request body streams; the daemon decides
// whether it must be buffered for substitution.
func (c *Client) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, errors.New("vault: nil request")
	}
	target := req.URL.String()
	out := req.Clone(req.Context())
	out.URL = &url.URL{Scheme: "http", Host: "vault", Path: "/v1/forward"}
	out.Method = http.MethodPost
	out.Host = ""
	out.TransferEncoding = nil
	header := http.Header{}
	header.Set(headerBot, c.bot)
	header.Set(headerMethod, req.Method)
	header.Set(headerURL, target)
	if req.Host != "" {
		header.Set(headerHost, req.Host)
	}
	for name, values := range req.Header {
		for _, v := range values {
			header.Add(headerHPrefix+name, v)
		}
	}
	out.Header = header
	// Keep the original body framing so the daemon can stream a large body
	// instead of chunking it into memory.
	out.ContentLength = req.ContentLength

	resp, err := c.transport().RoundTrip(out)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		// The daemon refused before forwarding (bad name, unbound host,
		// oversized placeholder body, ...). Its error text already went through
		// sanitizeError, so no value can appear here.
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		_ = resp.Body.Close()
		detail := strings.TrimSpace(string(msg))
		if detail == "" {
			detail = http.StatusText(resp.StatusCode)
		}
		return nil, fmt.Errorf("vault: daemon refused request (%d): %s", resp.StatusCode, detail)
	}
	return rebuildResponse(resp)
}

func rebuildResponse(resp *http.Response) (*http.Response, error) {
	code, err := strconv.Atoi(resp.Header.Get(headerStatus))
	if err != nil {
		_ = resp.Body.Close()
		return nil, errors.New("vault: bad daemon response")
	}
	header := http.Header{}
	for name, values := range resp.Header {
		if !strings.HasPrefix(name, headerHPrefix) {
			continue
		}
		real := strings.TrimPrefix(name, headerHPrefix)
		for _, v := range values {
			header.Add(real, v)
		}
	}
	return &http.Response{
		Status:     fmt.Sprintf("%d %s", code, http.StatusText(code)),
		StatusCode: code,
		Proto:      "HTTP/1.1",
		Header:     header,
		Body:       resp.Body,
	}, nil
}

// List returns names and domains only; the daemon never sends values.
func (c *Client) List() []Secret {
	raw, err := c.control(context.Background(), http.MethodGet, "/v1/list", nil)
	if err != nil {
		return nil
	}
	var entries []struct {
		Name    string   `json:"name"`
		Domains []string `json:"domains"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil
	}
	out := make([]Secret, 0, len(entries))
	for _, e := range entries {
		out = append(out, Secret{Name: e.Name, Domains: append([]string{}, e.Domains...)})
	}
	return out
}

// Names returns the stored secret names sorted.
func (c *Client) Names() []string {
	list := c.List()
	out := make([]string, 0, len(list))
	for _, sec := range list {
		out = append(out, sec.Name)
	}
	return out
}

// Set stores a value in the daemon's vault.
func (c *Client) Set(name, value string, domains []string) error {
	body, err := json.Marshal(setRequest{Name: name, Value: value, Domains: domains})
	if err != nil {
		return err
	}
	_, err = c.control(context.Background(), http.MethodPost, "/v1/set", body)
	return err
}

// Remove deletes a secret. A missing name is success.
func (c *Client) Remove(name string) error {
	body, err := json.Marshal(struct {
		Name string `json:"name"`
	}{Name: name})
	if err != nil {
		return err
	}
	_, err = c.control(context.Background(), http.MethodPost, "/v1/rm", body)
	return err
}

// Redact is a no-op on the client: no values are known here. The daemon already
// redacted every forwarded response.
func (c *Client) Redact(text string) string { return text }

// MaskTruncated is a no-op on the client, for the same reason as Redact.
func (c *Client) MaskTruncated(text string) string { return text }

// control runs one small JSON control call over the same persistent connection.
func (c *Client) control(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://vault"+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set(headerBot, c.bot)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.transport().RoundTrip(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxSetBody+1))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			msg = resp.Status
		}
		return nil, errors.New(msg)
	}
	return raw, nil
}
