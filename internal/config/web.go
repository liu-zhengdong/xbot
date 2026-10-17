package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"

	"xbot/internal/fsutil"
)

// WebFile is ~/.xbot/web.json. It names search and fetch backends.
// API keys are environment variable names, never values.
type WebFile struct {
	Search WebBackend `json:"search"`
	Fetch  WebBackend `json:"fetch"`
	// TrustedEgressCIDRs are prefixes a DNS answer may use even when the
	// address is not a public unicast (for example 198.18.0.0/15 when the
	// resolver rewrites every name). Literal IPs in URLs and redirects are
	// never allowed through this list. Loopback and link-local prefixes,
	// including cloud metadata, are rejected.
	TrustedEgressCIDRs []string `json:"trusted_egress_cidrs,omitempty"`
}

// WebBackend selects one backend. Backend empty means the keyless default
// (DuckDuckGo for search, plain HTTP for fetch).
type WebBackend struct {
	Backend   string `json:"backend,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`
	APIKeyEnv string `json:"api_key_env,omitempty"`
	// Model is the search model. Empty uses the backend default
	// (deepseek-v4-pro for the deepseek backend, because deepseek-flash
	// ignores the web_search tool).
	Model string `json:"model,omitempty"`
}

// WebPath is the web backend file under the data root.
func WebPath(root string) string { return filepath.Join(root, "web.json") }

// LoadWeb reads the web backend file. A missing file is the keyless default.
// XBOT_SEARCH_BACKEND, XBOT_SEARCH_BASE_URL, XBOT_SEARCH_MODEL, and
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
	applyWebEnv(&f, os.Getenv)
	if err := validateWeb(f); err != nil {
		return WebFile{}, err
	}
	return f, nil
}

// SaveWeb writes the web backend file. Callers must not put secrets in it.
func SaveWeb(root string, f WebFile) error {
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
	if v := strings.TrimSpace(getenv("XBOT_SEARCH_BACKEND")); v != "" {
		f.Search.Backend = v
	}
	if v := strings.TrimSpace(getenv("XBOT_SEARCH_BASE_URL")); v != "" {
		f.Search.BaseURL = v
	}
	if v := strings.TrimSpace(getenv("XBOT_SEARCH_API_KEY_ENV")); v != "" {
		f.Search.APIKeyEnv = v
	}
	if v := strings.TrimSpace(getenv("XBOT_SEARCH_MODEL")); v != "" {
		f.Search.Model = v
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
	if err := validateBackend("search", f.Search); err != nil {
		return err
	}
	if err := validateBackend("fetch", f.Fetch); err != nil {
		return err
	}
	_, err := TrustedEgress(f)
	return err
}

func validateBackend(kind string, b WebBackend) error {
	if b.APIKeyEnv != "" && !envName.MatchString(b.APIKeyEnv) {
		return fmt.Errorf("web: %s api_key_env must be an environment variable name, got %q", kind, b.APIKeyEnv)
	}
	return nil
}

// SearchKey reads the search backend's API key.
// A keyless backend returns an empty key. The deepseek backend uses
// api_key_env when it is set. Otherwise it reuses DEEPSEEK_API_KEY, then
// XBOT_API_KEY. Those provider keys are already held in memory; a separate
// name is held the same way via KeyEnvNames. The value is not logged.
func SearchKey(b WebBackend, getenv func(string) string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(b.Backend))
	if name == "" || name == "ddg" || name == "duckduckgo" || name == "http" {
		return "", nil
	}
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	if name != "deepseek" {
		return "", fmt.Errorf("web: unknown search backend %q", b.Backend)
	}
	if env := strings.TrimSpace(b.APIKeyEnv); env != "" {
		if !envName.MatchString(env) {
			return "", fmt.Errorf("web: search api_key_env must be an environment variable name, got %q", env)
		}
		key := strings.TrimSpace(getenv(env))
		if key == "" {
			return "", fmt.Errorf("web: %s is empty", env)
		}
		return key, nil
	}
	for _, candidate := range []string{"DEEPSEEK_API_KEY", "XBOT_API_KEY"} {
		if key := strings.TrimSpace(getenv(candidate)); key != "" {
			return key, nil
		}
	}
	return "", errors.New("web: deepseek search needs DEEPSEEK_API_KEY or XBOT_API_KEY, or set search.api_key_env")
}

// blockedEgress prefixes cannot be opted in. Loopback and link-local include
// the cloud metadata address.
var blockedEgress = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fe80::/10"),
}

// TrustedEgress parses trusted_egress_cidrs. An empty list is valid.
func TrustedEgress(f WebFile) ([]netip.Prefix, error) {
	if len(f.TrustedEgressCIDRs) == 0 {
		return nil, nil
	}
	out := make([]netip.Prefix, 0, len(f.TrustedEgressCIDRs))
	seen := map[string]bool{}
	for _, raw := range f.TrustedEgressCIDRs {
		p, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("web: trusted_egress_cidrs %q: %w", raw, err)
		}
		p = p.Masked()
		for _, bad := range blockedEgress {
			if p.Overlaps(bad) {
				return nil, fmt.Errorf("web: trusted_egress_cidrs %s overlaps %s; loopback and link-local stay blocked", p, bad)
			}
		}
		if seen[p.String()] {
			continue
		}
		seen[p.String()] = true
		out = append(out, p)
	}
	return out, nil
}
