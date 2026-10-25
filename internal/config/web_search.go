package config

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strings"

	"xbot/internal/fsutil"
)

// SearchProvider is the single catalog used by settings and local selection.
type SearchProvider struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	NeedsKey bool   `json:"needs_key"`
}

func SearchProviders() []SearchProvider {
	return []SearchProvider{
		{"ddg", "DuckDuckGo", false}, {"deepseek", "DeepSeek", true},
		{"brave", "Brave Search", true}, {"tavily", "Tavily", true}, {"serper", "Serper", true},
		{"zhipu", "智谱 · 联网搜索", true}, {"exa", "Exa", true}, {"firecrawl", "Firecrawl", true},
		{"perplexity", "Perplexity Search", true},
	}
}

func SearchProviderByID(id string) (SearchProvider, bool) {
	for _, p := range SearchProviders() {
		if p.ID == id {
			return p, true
		}
	}
	return SearchProvider{}, false
}

func (f WebFile) SearchOn() bool       { return f.SearchEnabled == nil || *f.SearchEnabled }
func (f WebFile) HostedSearchOn() bool { return f.SearchOn() && f.SearchMode != "local" }
func (f WebFile) SearchProviderID() string {
	id := strings.ToLower(strings.TrimSpace(f.Search.Backend))
	if id == "" || id == "duckduckgo" || id == "http" {
		return "ddg"
	}
	return id
}

// SearchSecretName is private to local search, so changing it never replaces a
// model's credential. Legacy explicit credentials remain usable until replaced.
func SearchSecretName(id string) string { return "XBOT_SEARCH_" + strings.ToUpper(id) + "_KEY" }

func SearchCredential(root string, f WebFile, id string) (string, error) {
	key, err := dedicatedSearchCredential(root, id)
	if err != nil || key != "" {
		return key, err
	}
	if id == "deepseek" {
		b := f.Search
		b.Backend = id
		key, _ := SearchKey(b, Getenv)
		return key, nil
	}
	return "", nil
}

func dedicatedSearchCredential(root, id string) (string, error) {
	var key string
	err := fsutil.WithLock(EnvFile(root)+".lock", func() error {
		var err error
		key, err = savedSearchCredential(root, id)
		if err != nil {
			return err
		}
		name := SearchSecretName(id)
		if key == "" {
			key = strings.TrimSpace(Getenv(name))
		}
		if key != "" {
			if old, _ := Held(name); old != key {
				SetHeld(name, key)
			}
		}
		return nil
	})
	return key, err
}

// SetWebSearch owns the read/modify/write, preserving fetch and egress settings.
func SetWebSearch(root string, enabled bool, mode, id string) error {
	if mode != "auto" && mode != "local" {
		return errors.New("怎么搜的选项不对")
	}
	if _, ok := SearchProviderByID(id); !ok {
		return errors.New("请从列表里选择一家")
	}
	return fsutil.WithLock(WebPath(root)+".lock", func() error {
		f, err := LoadWeb(root)
		if err != nil {
			return err
		}
		f.Search.Backend = id
		f.SearchEnabled, f.SearchMode = &enabled, mode
		return SaveWeb(root, f)
	})
}

func SaveSearchCredential(root, id, key string) error {
	p, ok := SearchProviderByID(id)
	if !ok || !p.NeedsKey {
		return errors.New("这家不需要密钥")
	}
	name := SearchSecretName(id)
	return fsutil.WithLock(EnvFile(root)+".lock", func() error {
		if err := saveEnvKey(root, name, key); err != nil {
			return err
		}
		SetHeld(name, key)
		return nil
	})
}

// SearchConnection binds the endpoint and key from one credential snapshot.
// A credential tested in settings can only be paired with the official endpoint.
func SearchConnection(root string, f WebFile) (WebBackend, string, error) {
	b := f.Search
	id := f.SearchProviderID()
	if id == "ddg" {
		return b, "", nil
	}
	key, err := dedicatedSearchCredential(root, id)
	if err != nil {
		return WebBackend{}, "", err
	}
	if key != "" {
		b.BaseURL, b.APIKeyEnv = "", SearchSecretName(id)
		return b, key, nil
	}
	key, err = SearchKey(b, Getenv)
	if id == "deepseek" && key == "" {
		return b, "", nil
	}
	return b, key, err
}

// Read only the unquoted, dedicated entries written by SaveEnvKey. This also
// works after a serve restart, which does not load the desktop env file.
func savedSearchCredential(root, id string) (string, error) {
	f, err := os.Open(EnvFile(root))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer f.Close()
	scan := bufio.NewScanner(io.LimitReader(f, 1<<20))
	prefix := SearchSecretName(id) + "="
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix), nil
		}
	}
	return "", scan.Err()
}

// HoldSearchCredentials runs before tools become available after a serve/app
// restart. Every stored provider participates in redaction, including unselected ones.
func HoldSearchCredentials(root string) error {
	return fsutil.WithLock(EnvFile(root)+".lock", func() error {
		for _, p := range SearchProviders() {
			if !p.NeedsKey {
				continue
			}
			key, err := savedSearchCredential(root, p.ID)
			if err != nil {
				return err
			}
			if key != "" {
				SetHeld(SearchSecretName(p.ID), key)
			}
		}
		return nil
	})
}
