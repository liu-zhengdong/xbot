package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"xbot/internal/vault"
)

const (
	defaultHTTPTimeout = 30 * time.Second
	maxHTTPBody        = 64 << 10
	// httpReadOverscan is read past maxHTTPBody so a value cut by the clip is
	// seen whole by the redactor and masked instead of leaking its prefix.
	httpReadOverscan = 8 << 10
)

// HTTPRequest makes one outbound request. The model may write
// {{secret:NAME}} in the URL, headers, or body; the vault proxy replaces it
// at egress and only for the domains bound to that secret. The value is never
// returned to the model.
type HTTPRequest struct {
	Store  vault.Backend
	Client *http.Client
}

func (h *HTTPRequest) Name() string { return "http_request" }

func (h *HTTPRequest) Description() string {
	return "Make an outbound HTTP request. Write {{secret:NAME}} where a stored credential belongs; the kernel substitutes it only when the host is bound to that secret. You never see the value."
}

func (h *HTTPRequest) Parameters() any {
	return objectSchema(map[string]any{
		"method": stringProp("HTTP method (default GET)"),
		"url":    stringProp("Absolute URL; may contain {{secret:NAME}}"),
		"headers": map[string]any{
			"type":                 "object",
			"additionalProperties": map[string]any{"type": "string"},
		},
		"body": stringProp("Request body; may contain {{secret:NAME}}"),
	}, []string{"url"})
}

type httpArgs struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

func (h *HTTPRequest) parse(args json.RawMessage) (httpArgs, string, error) {
	var in httpArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return httpArgs{}, "", fmt.Errorf("http_request: %w", err)
	}
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = http.MethodGet
	}
	if strings.TrimSpace(in.URL) == "" {
		return httpArgs{}, method, errors.New("http_request: url is required")
	}
	return in, method, nil
}

func (h *HTTPRequest) Confirm(args json.RawMessage) (bool, string, error) {
	in, method, err := h.parse(args)
	if err != nil {
		return false, "", err
	}
	// The kernel's central review forces this call when it injects a secret or
	// uses a method other than GET/HEAD. A plain read does not ask here.
	return false, fmt.Sprintf("%s %s", method, in.URL), nil
}

func (h *HTTPRequest) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	in, method, err := h.parse(args)
	if err != nil {
		return "", err
	}
	var body io.Reader
	if in.Body != "" {
		body = strings.NewReader(in.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, in.URL, body)
	if err != nil {
		return "", fmt.Errorf("http_request: %w", err)
	}
	for name, value := range in.Headers {
		req.Header.Set(name, value)
	}
	client := h.Client
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
		if h.Store != nil {
			client.Transport = h.Store.Transport(nil)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		if h.Store != nil {
			return "", fmt.Errorf("http_request: %s", h.Store.Redact(err.Error()))
		}
		return "", fmt.Errorf("http_request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPBody+httpReadOverscan+1))
	if err != nil {
		return "", fmt.Errorf("http_request: %w", err)
	}
	// Redact before clipping so a value that straddles the clip is masked
	// whole, then hide any prefix a longer value still leaves at the cut.
	text := string(raw)
	if h.Store != nil {
		text = h.Store.Redact(text)
	}
	if len(text) > maxHTTPBody {
		text = text[:maxHTTPBody]
		if h.Store != nil {
			text = h.Store.MaskTruncated(text)
		}
		text += "\n[truncated]"
	}
	return fmt.Sprintf("%s\n%s", resp.Status, text), nil
}
