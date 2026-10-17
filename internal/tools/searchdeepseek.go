package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// deepseekSearchURL is the official Search API. It takes a query and a
// result count. It does not use a model, the Responses API, or tokens.
// A successful request with a non-empty results list is billed per request
// (5 CNY per 1000), not per token. An empty results list is a success and
// is not billed.
// https://api-docs.deepseek.com/zh-cn/guides/search
const deepseekSearchURL = "https://api.deepseek.com/v1/tools/search"

const deepseekSearchTimeout = 45 * time.Second

const (
	deepseekQueryMax = 128
	deepseekCountMax = 50
)

var envVarName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// NewSearch builds the configured search backend.
// An empty backend, "ddg", and "duckduckgo" use DuckDuckGo and ignore the key.
// "deepseek" is the paid Search API: it runs only when selected. key must
// already be the secret value. The value is not logged. trusted applies only
// to DNS answers, never to literal IPs. The caller decides whether this key
// may be sent to baseURL.
func NewSearch(backend, baseURL, key string, trusted []netip.Prefix) (SearchBackend, error) {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "", "ddg", "duckduckgo":
		return DDG{Guard: guardWithTrusted(trusted)}, nil
	case "deepseek":
		if strings.TrimSpace(key) == "" {
			return nil, errors.New("search: api key is empty")
		}
		g := guardWithTrusted(trusted)
		if g == nil {
			g = &Guard{}
		}
		if g.Timeout <= 0 {
			g.Timeout = deepseekSearchTimeout
		}
		if g.MaxBytes <= 0 {
			g.MaxBytes = 256 << 10
		}
		return deepseekSearch{
			BaseURL: strings.TrimSpace(baseURL),
			Key:     key,
			Guard:   g,
		}, nil
	default:
		return nil, fmt.Errorf("search: unknown backend %q", backend)
	}
}

// NewFetcher builds the configured page fetcher. Only "http" is enabled.
// Any other name, including a future paid backend, fails closed so it cannot
// become the default by being selected accidentally.
func NewFetcher(backend string, trusted []netip.Prefix) (Fetcher, error) {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "", "http":
		return HTTPFetch{Guard: guardWithTrusted(trusted)}, nil
	default:
		return nil, fmt.Errorf("fetch: backend %q is not enabled", backend)
	}
}

func guardWithTrusted(trusted []netip.Prefix) *Guard {
	if len(trusted) == 0 {
		return nil
	}
	return &Guard{Trusted: append([]netip.Prefix(nil), trusted...)}
}

// EnvName reports whether s is an environment variable name, not a secret.
func EnvName(s string) bool { return envVarName.MatchString(s) }

type deepseekSearch struct {
	BaseURL string
	Key     string
	Guard   *Guard
}

func (d deepseekSearch) Search(ctx context.Context, query string, n int) ([]Hit, error) {
	query = strings.TrimSpace(query)
	if query == "" || utf8.RuneCountInString(query) > deepseekQueryMax {
		return nil, errors.New("deepseek search: query must be 1 to 128 characters")
	}
	if n < 1 {
		n = 1
	}
	if n > deepseekCountMax {
		n = deepseekCountMax
	}
	payload, err := json.Marshal(deepseekSearchRequest{Query: query, Count: n})
	if err != nil {
		return nil, err
	}
	g := d.guard()
	h := make(http.Header)
	h.Set("Authorization", "Bearer "+d.Key)
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "application/json")
	res, err := g.Post(ctx, deepseekEndpoint(d.BaseURL), payload, h)
	if err != nil {
		return nil, scrubKey(err, d.Key)
	}
	if res.Status != http.StatusOK {
		return nil, scrubKey(deepseekHTTPError(res.Status, res.Body), d.Key)
	}
	hits, err := parseDeepSeekSearch(res.Body, n)
	if err != nil {
		return nil, scrubKey(err, d.Key)
	}
	return hits, nil
}

func (d deepseekSearch) guard() *Guard {
	g := d.Guard
	if g == nil {
		g = &Guard{}
	}
	if g.Timeout > 0 && g.MaxBytes > 0 {
		return g
	}
	cp := *g
	if cp.Timeout <= 0 {
		cp.Timeout = deepseekSearchTimeout
	}
	if cp.MaxBytes <= 0 {
		cp.MaxBytes = 256 << 10
	}
	return &cp
}

type deepseekSearchRequest struct {
	Query string `json:"query"`
	Count int    `json:"count"`
}

func deepseekEndpoint(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return deepseekSearchURL
	}
	low := strings.ToLower(base)
	if strings.HasSuffix(low, "/v1/tools/search") {
		return base
	}
	if strings.HasSuffix(low, "/v1") {
		return base + "/tools/search"
	}
	return base + "/v1/tools/search"
}

func deepseekHTTPError(status int, body []byte) error {
	switch status {
	case http.StatusPaymentRequired:
		return errors.New("deepseek search: insufficient balance (HTTP 402). The owner must top up the DeepSeek account. Do not retry this request unchanged")
	case http.StatusTooManyRequests:
		return errors.New("deepseek search: rate limited (HTTP 429). Tell the owner and wait before retrying")
	}
	msg := clipPublic(body)
	if msg == "" {
		return fmt.Errorf("deepseek search status %d", status)
	}
	return fmt.Errorf("deepseek search status %d: %s", status, msg)
}

func parseDeepSeekSearch(raw []byte, n int) ([]Hit, error) {
	var resp struct {
		Results []deepseekResult `json:"results"`
		Error   *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, errors.New("deepseek search: bad response")
	}
	if resp.Error != nil && strings.TrimSpace(resp.Error.Message) != "" {
		return nil, errors.New("deepseek search failed: " + clipPublic([]byte(resp.Error.Message)))
	}
	hits := make([]Hit, 0, len(resp.Results))
	seen := map[string]bool{}
	for _, src := range resp.Results {
		h := src.hit()
		if h.URL == "" && h.Title == "" && h.Snippet == "" && h.Summary == "" {
			continue
		}
		key := h.URL
		if key == "" {
			key = h.Title + "\n" + h.Snippet
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		hits = append(hits, h)
	}
	if n > 0 && len(hits) > n {
		hits = hits[:n]
	}
	return hits, nil
}

type deepseekResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Snippet string  `json:"snippet"`
	Summary string  `json:"summary"`
	Date    *string `json:"date"`
}

func (s deepseekResult) hit() Hit {
	h := Hit{
		Title:   strings.TrimSpace(s.Title),
		URL:     strings.TrimSpace(s.URL),
		Snippet: strings.TrimSpace(s.Snippet),
		Summary: strings.TrimSpace(s.Summary),
	}
	if s.Date != nil {
		h.Date = strings.TrimSpace(*s.Date)
	}
	return h
}

func clipPublic(b []byte) string {
	s := strings.ToValidUTF8(string(b), "")
	if len(s) > 180 {
		s = s[:180]
		for len(s) > 0 && !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	s = strings.Map(func(r rune) rune {
		if r < 32 {
			return ' '
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

func scrubKey(err error, key string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if key != "" {
		msg = strings.ReplaceAll(msg, key, "[secret]")
	}
	return errors.New(msg)
}
