package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"xbot/internal/fsutil"
)

// WebFile is ~/.xbot/web.json. It names search and fetch backends.
// API keys are environment variable names, never values.
type WebFile struct {
	Search        WebBackend `json:"search"`
	Fetch         WebBackend `json:"fetch"`
	SearchEnabled *bool      `json:"search_enabled,omitempty"`
	SearchMode    string     `json:"search_mode,omitempty"`
	// TrustedEgressCIDRs is accepted so an older web.json still loads. Web
	// requests no longer need an address allow-list. The value is ignored
	// and dropped on the next save.
	TrustedEgressCIDRs []string `json:"trusted_egress_cidrs,omitempty"`
}

// WebBackend selects one backend. Backend empty means the keyless default
// (DuckDuckGo for search, plain HTTP for fetch).
type WebBackend struct {
	Backend   string `json:"backend,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`
	APIKeyEnv string `json:"api_key_env,omitempty"`
	// Model is accepted so an older web.json still loads. Search uses the
	// Search API, which has no model. The value is ignored.
	Model string `json:"model,omitempty"`
}

// WebPath is the web backend file under the data root.
func WebPath(root string) string { return filepath.Join(root, "web.json") }

// LoadWeb reads the web backend file. A missing file is the keyless default.
// XBOT_SEARCH_BACKEND, XBOT_SEARCH_BASE_URL, and
// XBOT_SEARCH_API_KEY_ENV override the search section when set. The fetch section uses
// XBOT_FETCH_BACKEND, XBOT_FETCH_BASE_URL, and XBOT_FETCH_API_KEY_ENV.
// None of these variables is a secret; the secret is the variable they name.
func LoadWeb(root string) (WebFile, error) {
	f := WebFile{}
	if strings.TrimSpace(root) != "" {
		raw, err := os.ReadFile(WebPath(root))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return WebFile{}, err
		}
		if err == nil {
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&f); err != nil {
				return WebFile{}, fmt.Errorf("web: %w", err)
			}
		}
	}
	f.TrustedEgressCIDRs = nil
	applyWebEnv(&f, os.Getenv)
	if err := validateWeb(f); err != nil {
		return WebFile{}, err
	}
	return f, nil
}

// SaveWeb writes the web backend file. Callers must not put secrets in it.
func SaveWeb(root string, f WebFile) error {
	f.TrustedEgressCIDRs = nil
	if err := validateWeb(f); err != nil {
		return err
	}
	raw, err := fsutil.Marshal(f)
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(WebPath(root), raw, 0600)
}

func applyWebEnv(f *WebFile, getenv func(string) string) {
	if v := strings.TrimSpace(getenv("XBOT_SEARCH_BACKEND")); v != "" && f.SearchEnabled == nil {
		f.Search.Backend = v
	}
	if v := strings.TrimSpace(getenv("XBOT_SEARCH_BASE_URL")); v != "" && f.SearchEnabled == nil {
		f.Search.BaseURL = v
	}
	if v := strings.TrimSpace(getenv("XBOT_SEARCH_API_KEY_ENV")); v != "" && f.SearchEnabled == nil {
		f.Search.APIKeyEnv = v
	}
	if v := strings.TrimSpace(getenv("XBOT_FETCH_BACKEND")); v != "" {
		f.Fetch.Backend = v
	}
	if v := strings.TrimSpace(getenv("XBOT_FETCH_BASE_URL")); v != "" {
		f.Fetch.BaseURL = v
	}
	if v := strings.TrimSpace(getenv("XBOT_FETCH_API_KEY_ENV")); v != "" {
		f.Fetch.APIKeyEnv = v
	}
}

func validateWeb(f WebFile) error {
	if f.SearchMode != "" && f.SearchMode != "auto" && f.SearchMode != "local" {
		return errors.New("web: invalid search mode")
	}
	if err := validateBackend("search", f.Search); err != nil {
		return err
	}
	return validateBackend("fetch", f.Fetch)
}

func validateBackend(kind string, b WebBackend) error {
	if b.APIKeyEnv != "" && !envName.MatchString(b.APIKeyEnv) {
		return fmt.Errorf("web: %s api_key_env must be an environment variable name, got %q", kind, b.APIKeyEnv)
	}
	return nil
}

// SearchKey reads the search backend's API key.
// A keyless backend returns an empty key. The deepseek backend uses
// api_key_env when it is set. Otherwise it uses DEEPSEEK_API_KEY only.
// It does not fall back to XBOT_API_KEY. The value is not logged.
func SearchKey(b WebBackend, getenv func(string) string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(b.Backend))
	if name != "" && name != "ddg" && name != "duckduckgo" && name != "http" && name != "deepseek" {
		return "", fmt.Errorf("web: unknown search backend %q", b.Backend)
	}
	env, ok := SearchKeyEnv(b)
	if !ok {
		return "", nil
	}
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	if !envName.MatchString(env) {
		return "", fmt.Errorf("web: search api_key_env must be an environment variable name, got %q", env)
	}
	key := strings.TrimSpace(getenv(env))
	if key == "" {
		if env == "DEEPSEEK_API_KEY" {
			return "", errors.New("web: deepseek search needs DEEPSEEK_API_KEY, or set search.api_key_env")
		}
		return "", fmt.Errorf("web: %s is empty", env)
	}
	return key, nil
}

// SearchKeyEnv is the environment variable the deepseek backend reads.
// The bool is false for a keyless backend. An unknown backend returns an error
// from SearchKey, not from here.
func SearchKeyEnv(b WebBackend) (string, bool) {
	name := strings.ToLower(strings.TrimSpace(b.Backend))
	if name == "" || name == "ddg" || name == "duckduckgo" || name == "http" {
		return "", false
	}
	if name != "deepseek" {
		return "", false
	}
	if env := strings.TrimSpace(b.APIKeyEnv); env != "" {
		return env, true
	}
	return "DEEPSEEK_API_KEY", true
}

// AllowSearchBase reports whether a search key may be sent to base_url.
// A keyless backend is always allowed. An empty base is the official
// https://api.deepseek.com endpoint. http is rejected whenever a key is sent.
// A provider key is sent only to https://api.deepseek.com. That is
// DEEPSEEK_API_KEY, XBOT_API_KEY, a providers.json api_key_env, and every
// name when providers.json cannot be loaded. A held search key that is not
// one of those may use another https host.
func AllowSearchBase(root string, b WebBackend) error {
	env, ok := SearchKeyEnv(b)
	if !ok {
		if strings.EqualFold(strings.TrimSpace(b.Backend), "deepseek") {
			return errors.New("web: deepseek search needs DEEPSEEK_API_KEY, or set search.api_key_env")
		}
		if name := strings.ToLower(strings.TrimSpace(b.Backend)); name != "" && name != "ddg" && name != "duckduckgo" && name != "http" {
			return fmt.Errorf("web: unknown search backend %q", b.Backend)
		}
		return nil
	}
	return checkSearchBase(providerSearchEnv(root, env), b.BaseURL)
}

func providerSearchEnv(root, env string) bool {
	if env == "DEEPSEEK_API_KEY" || env == "XBOT_API_KEY" {
		return true
	}
	// A registry we cannot read might list this name. Fail closed.
	// Held names and KeyEnvNames are not used here: both include the search
	// key itself, which is allowed to use a dedicated https host.
	f, err := Load(root)
	if err != nil {
		return true
	}
	for _, p := range f.Providers {
		if p.APIKeyEnv == env {
			return true
		}
	}
	return false
}

func checkSearchBase(provider bool, base string) error {
	base = strings.TrimSpace(base)
	if base == "" {
		return nil
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.Scheme == "" {
		return fmt.Errorf("web: search base_url %q is not a valid URL", base)
	}
	if u.User != nil {
		return errors.New("web: search base_url must not include user info")
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("web: search base_url must be https, got %s", u.Scheme)
	}
	if provider && !strings.EqualFold(u.Hostname(), "api.deepseek.com") {
		return fmt.Errorf("web: a provider key is only sent to https://api.deepseek.com, not %s", u.Hostname())
	}
	return nil
}
