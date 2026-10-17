package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

const deepseekSearchURL = "https://api.deepseek.com/v1/search"

var envVarName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// NewSearch builds the configured search backend.
// An empty backend, "ddg", and "duckduckgo" use DuckDuckGo and ignore the key.
// "deepseek" is the paid backend: it runs only when selected, and key must
// already be the secret value. The value is not logged.
func NewSearch(backend, baseURL, key string) (SearchBackend, error) {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "", "ddg", "duckduckgo":
		return DDG{}, nil
	case "deepseek":
		if strings.TrimSpace(key) == "" {
			return nil, errors.New("search: api key is empty")
		}
		return deepseekSearch{BaseURL: strings.TrimSpace(baseURL), Key: key}, nil
	default:
		return nil, fmt.Errorf("search: unknown backend %q", backend)
	}
}

// NewFetcher builds the configured page fetcher. Only "http" is enabled.
// Any other name, including a future paid backend, fails closed so it cannot
// become the default by being selected accidentally.
func NewFetcher(backend string) (Fetcher, error) {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "", "http":
		return HTTPFetch{}, nil
	default:
		return nil, fmt.Errorf("fetch: backend %q is not enabled", backend)
	}
}

// EnvName reports whether s is an environment variable name, not a secret.
func EnvName(s string) bool { return envVarName.MatchString(s) }

type deepseekSearch struct {
	BaseURL string
	Key     string
	Guard   *Guard
}

type deepseekHit struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

func (d deepseekSearch) Search(ctx context.Context, query string, n int) ([]Hit, error) {
	payload, err := json.Marshal(struct {
		Query string `json:"query"`
		N     int    `json:"n"`
	}{Query: query, N: n})
	if err != nil {
		return nil, err
	}
	endpoint := deepseekSearchURL
	if d.BaseURL != "" {
		endpoint = strings.TrimRight(d.BaseURL, "/") + "/v1/search"
	}
	g := d.Guard
	if g == nil {
		g = &Guard{MaxBytes: 256 << 10}
	}
	h := make(http.Header)
	h.Set("Authorization", "Bearer "+d.Key)
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "application/json")
	res, err := g.Post(ctx, endpoint, payload, h)
	if err != nil {
		return nil, scrubKey(err, d.Key)
	}
	if res.Status < 200 || res.Status >= 300 {
		return nil, fmt.Errorf("deepseek search status %d", res.Status)
	}
	var body struct {
		Results []deepseekHit `json:"results"`
	}
	if err := json.Unmarshal(res.Body, &body); err != nil {
		return nil, errors.New("deepseek search: bad response")
	}
	out := make([]Hit, 0, len(body.Results))
	for _, hit := range body.Results {
		out = append(out, Hit{Title: hit.Title, URL: hit.URL, Snippet: hit.Snippet})
	}
	return out, nil
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
