package gateway

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPError preserves the status for callers deciding whether to retry.
type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string { return e.Message }

// Client authenticates requests with one installation's key.
type Client struct {
	Base string
	Key  ed25519.PrivateKey
	HTTP *http.Client
	Now  func() time.Time
}

func (c *Client) Register(ctx context.Context, token string) error {
	return c.request(ctx, "/v1/register", nil, strings.TrimSpace(token), 15*time.Second, nil)
}

func (c *Client) RegisterCode(ctx context.Context, lookup string, ttl time.Duration) error {
	return c.request(ctx, "/v1/code", codeInput{lookup, int(ttl / time.Second)}, "", 15*time.Second, nil)
}
func (c *Client) CancelCode(ctx context.Context, lookup string) error {
	return c.request(ctx, "/v1/code/cancel", codeInput{Lookup: lookup}, "", 15*time.Second, nil)
}

// ClaimCode returns the owner's install id; the caller becomes a pending install.
func (c *Client) ClaimCode(ctx context.Context, lookup string) (owner string, err error) {
	var out struct {
		Install string `json:"install"`
	}
	err = c.request(ctx, "/v1/code/claim", codeInput{Lookup: lookup}, "", 15*time.Second, &out)
	return out.Install, err
}

// AdmitCode promotes a pending install that claimed this owner's code.
func (c *Client) AdmitCode(ctx context.Context, install string) error {
	return c.request(ctx, "/v1/code/admit", map[string]string{"install": install}, "", 15*time.Second, nil)
}
func (c *Client) Send(ctx context.Context, env Envelope) error {
	return c.request(ctx, "/v1/send", env, "", 15*time.Second, nil)
}
func (c *Client) Ack(ctx context.Context, ids []string) error {
	return c.request(ctx, "/v1/ack", map[string][]string{"ids": ids}, "", 15*time.Second, nil)
}
func (c *Client) Poll(ctx context.Context, wait time.Duration) ([]Envelope, error) {
	secs := clampWait(int(wait / time.Second))
	var out struct {
		Envelopes []Envelope `json:"envelopes"`
	}
	err := c.request(ctx, fmt.Sprintf("/v1/poll?wait=%d", secs), nil, "", time.Duration(secs)*time.Second+15*time.Second, &out)
	if err == nil && out.Envelopes == nil {
		err = errors.New("gateway: missing envelopes")
	}
	return out.Envelopes, err
}

func clampWait(secs int) int { return max(0, min(secs, int(MaxWait/time.Second))) }

func (c *Client) request(ctx context.Context, path string, in any, token string, timeout time.Duration, out any) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var body []byte
	var err error
	if in != nil {
		body, err = json.Marshal(in)
		if err != nil {
			return err
		}
	}
	method := http.MethodPost
	if strings.HasPrefix(path, "/v1/poll?") {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Base, "/")+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	SignRequest(req, c.Key, body, now)
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var je struct {
			Error string `json:"error"`
		}
		msg := strings.TrimSpace(string(raw))
		if json.Unmarshal(raw, &je) == nil && je.Error != "" {
			msg = je.Error
		}
		return &HTTPError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("gateway: %s: %.200s", resp.Status, msg)}
	}
	if err != nil {
		return err
	}
	if len(raw) > 1<<20 {
		return errors.New("gateway: response too large")
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}
