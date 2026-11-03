package cliops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const jsonRequestLimit = 1 << 20
const jsonReplyLimit = 64 << 10

// decodePOSTJSON validates the wire once. Domain rules remain with the callback.
func decodePOSTJSON(w http.ResponseWriter, r *http.Request, operation string, input any) (int, error) {
	if r.Method != http.MethodPost {
		return http.StatusMethodNotAllowed, fmt.Errorf("%s: requires POST", operation)
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return http.StatusBadRequest, fmt.Errorf("%s: query fields are not supported", operation)
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, jsonRequestLimit))
	if err != nil {
		return http.StatusBadRequest, fmt.Errorf("%s: invalid request or request exceeds 1 MiB", operation)
	}
	raw = bytes.TrimLeft(raw, " \t\r\n")
	if len(raw) == 0 || raw[0] != '{' {
		return http.StatusBadRequest, fmt.Errorf("%s: request must be one JSON object", operation)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		return http.StatusBadRequest, fmt.Errorf("%s: invalid request fields", operation)
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return http.StatusBadRequest, fmt.Errorf("%s: request must be one JSON object followed by EOF", operation)
	}
	wire, err := json.Marshal(input)
	if err != nil || len(wire) > jsonRequestLimit {
		return http.StatusBadRequest, fmt.Errorf("%s: request exceeds 1 MiB after JSON encoding", operation)
	}
	return 0, nil
}

// postJSON keeps non-2xx bodies for typed callers to preserve actual receipts.
// Call IDs are explicit correlation data; this function never reads the environment.
func postJSON(ctx context.Context, endpoint, root, operation, path, callID string, input any) ([]byte, int, error) {
	root, err := sourceRoot(endpoint, root, operation)
	if err != nil {
		return nil, 0, err
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, 0, err
	}
	if len(body) > jsonRequestLimit {
		return nil, 0, fmt.Errorf("%s: request exceeds 1 MiB after JSON encoding", operation)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+path, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("X-XBot-Root", root)
	req.Header.Set("Content-Type", "application/json")
	if callID != "" {
		req.Header.Set("X-XBot-Call-ID", callID)
	}
	client := LocalHTTPClient(0)
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: runtime request: %w", operation, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, jsonReplyLimit+1))
	if err != nil || len(raw) > jsonReplyLimit {
		return nil, 0, fmt.Errorf("%s: invalid runtime response or response exceeds 64 KiB", operation)
	}
	return raw, response.StatusCode, nil
}

// LocalHTTPClient preserves each caller's timeout while avoiding ambient proxies
// and redirects for explicit local runtime operations.
func LocalHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Transport:     &http.Transport{Proxy: nil},
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
