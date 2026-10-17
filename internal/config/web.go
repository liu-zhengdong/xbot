package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
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
// A provider key (DEEPSEEK_API_KEY, XBOT_API_KEY, or a providers.json
// api_key_env) is sent only to https://api.deepseek.com.
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
	f, err := Load(root)
	if err != nil {
		return false
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
		if bad, ok := overlapsBlocked(p); ok {
			return nil, fmt.Errorf("web: trusted_egress_cidrs %s overlaps %s; loopback and link-local stay blocked", p, bad)
		}
		if seen[p.String()] {
			continue
		}
		seen[p.String()] = true
		out = append(out, p)
	}
	return out, nil
}

// overlapsBlocked compares a prefix with blocked ranges of the same family.
// An IPv6 prefix that covers IPv4-mapped addresses (::ffff:0:0/96) is also
// checked against the IPv4 ranges, so ::ffff:127.0.0.0/104 cannot opt in
// loopback. Cross-family Overlaps is false, which is why the projection exists.
func overlapsBlocked(p netip.Prefix) (netip.Prefix, bool) {
	p = p.Masked()
	for _, bad := range blockedEgress {
		if p.Addr().BitLen() == bad.Addr().BitLen() && p.Overlaps(bad) {
			return bad, true
		}
	}
	if !p.Addr().Is6() {
		return netip.Prefix{}, false
	}
	mapped := netip.MustParsePrefix("::ffff:0:0/96")
	if !p.Overlaps(mapped) {
		return netip.Prefix{}, false
	}
	v4, ok := projectMappedV4(p)
	if !ok {
		return netip.Prefix{}, false
	}
	for _, bad := range blockedEgress {
		if bad.Addr().Is4() && v4.Overlaps(bad) {
			return bad, true
		}
	}
	return netip.Prefix{}, false
}

func projectMappedV4(p netip.Prefix) (netip.Prefix, bool) {
	bits := p.Bits()
	if bits < 96 {
		return netip.MustParsePrefix("0.0.0.0/0"), true
	}
	ip := p.Addr().As16()
	addr := netip.AddrFrom4([4]byte{ip[12], ip[13], ip[14], ip[15]})
	v4bits := bits - 96
	if v4bits > 32 {
		return netip.Prefix{}, false
	}
	return netip.PrefixFrom(addr, v4bits).Masked(), true
}
