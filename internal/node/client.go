package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Ping reads GET /v1/node.
func Ping(ctx context.Context, baseURL, token string) (Info, error) {
	var info Info
	err := do(ctx, http.MethodGet, baseURL, token, "/v1/node", nil, &info)
	return info, err
}

// Call runs one capability and returns its text.
func Call(ctx context.Context, baseURL, token, cap string) (string, error) {
	var out struct {
		Text string `json:"text"`
	}
	body, err := json.Marshal(map[string]string{"cap": cap})
	if err != nil {
		return "", err
	}
	if err := do(ctx, http.MethodPost, baseURL, token, "/v1/call", body, &out); err != nil {
		return "", err
	}
	return out.Text, nil
}

func do(ctx context.Context, method, baseURL, token, path string, body []byte, dest any) error {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return errors.New("node: empty url")
	}
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, r)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("node: redirect")
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		var env struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &env)
		if env.Error == "" {
			env.Error = strings.TrimSpace(string(raw))
		}
		if env.Error == "" {
			env.Error = resp.Status
		}
		return fmt.Errorf("node: %s", env.Error)
	}
	if dest == nil {
		return nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return err
	}
	return nil
}
