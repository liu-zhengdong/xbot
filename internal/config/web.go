package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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
}

// WebBackend selects one backend. Backend empty means the keyless default
// (DuckDuckGo for search, plain HTTP for fetch).
type WebBackend struct {
	Backend   string `json:"backend,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`
	APIKeyEnv string `json:"api_key_env,omitempty"`
}

// WebPath is the web backend file under the data root.
func WebPath(root string) string { return filepath.Join(root, "web.json") }

// LoadWeb reads the web backend file. A missing file is the keyless default.
// XBOT_SEARCH_BACKEND, XBOT_SEARCH_BASE_URL, and XBOT_SEARCH_API_KEY_ENV
// override the search section when set. The fetch section uses
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
	return validateBackend("fetch", f.Fetch)
}

func validateBackend(kind string, b WebBackend) error {
	if b.APIKeyEnv != "" && !envName.MatchString(b.APIKeyEnv) {
		return fmt.Errorf("web: %s api_key_env must be an environment variable name, got %q", kind, b.APIKeyEnv)
	}
	return nil
}

// SearchKey reads the search backend's API key from the environment.
// A keyless backend returns an empty key. A provider key held for the model
// endpoint is refused: that value must not be copied into another header.
func SearchKey(b WebBackend, getenv func(string) string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(b.Backend))
	if name == "" || name == "ddg" || name == "duckduckgo" || name == "http" {
		return "", nil
	}
	if b.APIKeyEnv == "" {
		return "", fmt.Errorf("web: %s backend %q needs api_key_env", "search", b.Backend)
	}
	if ProviderKey(b.APIKeyEnv) {
		return "", fmt.Errorf("web: %s is a model provider key; set a separate variable for search", b.APIKeyEnv)
	}
	key := strings.TrimSpace(getenv(b.APIKeyEnv))
	if key == "" {
		return "", fmt.Errorf("web: %s is empty", b.APIKeyEnv)
	}
	return key, nil
}
