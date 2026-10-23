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
	"regexp"
	"strings"

	"xbot/internal/fsutil"
	"xbot/internal/provider"
)

// envName is an environment variable name, never a secret.
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Provider is one protocol endpoint. The key is the name of an environment variable.
type Provider struct {
	Name      string      `json:"name"`
	Protocol  string      `json:"protocol,omitempty"`
	BaseURL   string      `json:"base_url"`
	APIKeyEnv string      `json:"api_key_env"`
	Models    []ModelSpec `json:"models"`
}

// ModelSpec is one model id and its context length in tokens.
// Todo turns the todo reminder on or off. Nil means the default: on when the
// provider or model name contains "deepseek", off otherwise.
// TodoSteps is how many tool steps a list must sit unchanged before a reminder.
// Zero means the default of 6.
// Thinking is "off" (send thinking disabled), "omit" (send no thinking field),
// or empty: off for DeepSeek model names, omit for the rest.
// Vision, when set, is this model's image understanding. Nil uses the default
// for the model id: deepseek-flash sees images; deepseek-chat and
// deepseek-v4-pro do not. Other ids do not, until the spec says so.
type ModelSpec struct {
	Name         string `json:"name"`
	Context      int    `json:"context"`
	Todo         *bool  `json:"todo,omitempty"`
	TodoSteps    int    `json:"todo_steps,omitempty"`
	Thinking     string `json:"thinking,omitempty"`
	Vision       *bool  `json:"vision,omitempty"`
	HostedSearch *bool  `json:"hosted_web_search,omitempty"`
}

// File is ~/.xbot/providers.json. It must not contain raw API keys.
type File struct {
	Providers []Provider `json:"providers"`
}

// Path is the registry file under the data root.
func Path(root string) string { return filepath.Join(root, "providers.json") }

// KeyEnvNames is XBOT_API_KEY, each provider's api_key_env, and any search or
// fetch api_key_env from web.json (including names selected by
// XBOT_SEARCH_API_KEY_ENV and XBOT_FETCH_API_KEY_ENV). DeepSeek search with
// no api_key_env also names DEEPSEEK_API_KEY. A missing registry still
// returns XBOT_API_KEY. configureWeb calls HoldKeys again so a web.json
// written after startup is covered.
func KeyEnvNames(root string) []string {
	names := []string{"XBOT_API_KEY"}
	seen := map[string]bool{"XBOT_API_KEY": true}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] || !envName.MatchString(name) {
			return
		}
		seen[name] = true
		names = append(names, name)
	}
	if f, err := Load(root); err == nil {
		for _, p := range f.Providers {
			add(p.APIKeyEnv)
		}
	}
	if w, err := LoadWeb(root); err == nil {
		add(w.Search.APIKeyEnv)
		add(w.Fetch.APIKeyEnv)
		if env, ok := SearchKeyEnv(w.Search); ok {
			add(env)
		}
	} else {
		add(os.Getenv("XBOT_SEARCH_API_KEY_ENV"))
		add(os.Getenv("XBOT_FETCH_API_KEY_ENV"))
	}
	return names
}

// Load reads the registry. A missing file is an empty registry.
func Load(root string) (File, error) {
	raw, err := os.ReadFile(Path(root))
	if errors.Is(err, os.ErrNotExist) {
		return File{}, nil
	}
	if err != nil {
		return File{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var f File
	if err := dec.Decode(&f); err != nil {
		return File{}, fmt.Errorf("providers: %w", err)
	}
	return f, nil
}

// Save writes the registry. Callers must not put secrets in it.
func Save(root string, f File) error {
	raw, err := fsutil.Marshal(f)
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(Path(root), raw, 0600)
}

// Add upserts a provider and one model. A second add with the same model updates its context length.
func Add(root string, p Provider, model ModelSpec) error {
	if !nameOK(p.Name) {
		return fmt.Errorf("providers: bad name %q", p.Name)
	}
	if err := validateModel(model); err != nil {
		return err
	}
	f, err := Load(root)
	if err != nil {
		return err
	}
	i := indexOf(f.Providers, p.Name)
	if i < 0 {
		if err := validateProvider(p, false); err != nil {
			return err
		}
		p.BaseURL = strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
		p.Models = []ModelSpec{model}
		f.Providers = append(f.Providers, p)
		return Save(root, f)
	}
	cur := f.Providers[i]
	if p.Protocol != "" {
		if err := ValidProtocol(p.Protocol); err != nil {
			return err
		}
		cur.Protocol = p.Protocol
	}
	if p.BaseURL != "" {
		if err := checkProviderBase(p.BaseURL); err != nil {
			return err
		}
		cur.BaseURL = strings.TrimRight(p.BaseURL, "/")
	}
	if p.APIKeyEnv != "" {
		cur.APIKeyEnv = p.APIKeyEnv
	}
	replaced := false
	for n := range cur.Models {
		if cur.Models[n].Name == model.Name {
			// A context-length update leaves an explicit todo switch in place
			// unless the caller set one.
			if model.Todo == nil {
				model.Todo = cur.Models[n].Todo
			}
			if model.TodoSteps == 0 {
				model.TodoSteps = cur.Models[n].TodoSteps
			}
			if model.Thinking == "" {
				model.Thinking = cur.Models[n].Thinking
			}
			if model.Vision == nil {
				model.Vision = cur.Models[n].Vision
			}
			if model.HostedSearch == nil {
				model.HostedSearch = cur.Models[n].HostedSearch
			}
			cur.Models[n] = model
			replaced = true
			break
		}
	}
	if !replaced {
		cur.Models = append(cur.Models, model)
	}
	f.Providers[i] = cur
	return Save(root, f)
}

// Remove deletes a provider, or one model when model is not empty.
// Removing the last model deletes the provider.
func Remove(root, name, model string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("providers: name is required")
	}
	f, err := Load(root)
	if err != nil {
		return err
	}
	i := indexOf(f.Providers, name)
	if i < 0 {
		return fmt.Errorf("providers: %q not found", name)
	}
	if model == "" {
		f.Providers = append(f.Providers[:i], f.Providers[i+1:]...)
		return Save(root, f)
	}
	cur := f.Providers[i]
	next := cur.Models[:0]
	found := false
	for _, m := range cur.Models {
		if m.Name == model {
			found = true
			continue
		}
		next = append(next, m)
	}
	if !found {
		return fmt.Errorf("providers: model %q not found on %q", model, name)
	}
	if len(next) == 0 {
		f.Providers = append(f.Providers[:i], f.Providers[i+1:]...)
	} else {
		cur.Models = next
		f.Providers[i] = cur
	}
	return Save(root, f)
}

// Lookup finds a named provider and model. It does not read the API key.
func Lookup(root, providerName, modelName string) (Provider, ModelSpec, error) {
	f, err := Load(root)
	if err != nil {
		return Provider{}, ModelSpec{}, err
	}
	i := indexOf(f.Providers, providerName)
	if i < 0 {
		return Provider{}, ModelSpec{}, fmt.Errorf("providers: %q not found", providerName)
	}
	p := f.Providers[i]
	if modelName == "" && len(p.Models) == 1 {
		return p, p.Models[0], nil
	}
	for _, m := range p.Models {
		if m.Name == modelName {
			return p, m, nil
		}
	}
	return Provider{}, ModelSpec{}, fmt.Errorf("providers: model %q is not on %q", modelName, providerName)
}

// Resolve turns a bot binding into an endpoint. An empty binding uses FromEnv and a zero context length.
// The API key is read from the environment variable named in the registry.
func Resolve(root, providerName, modelName string, getenv func(string) string) (endpoint ModelConfig, contextLen int, err error) {
	providerName = strings.TrimSpace(providerName)
	modelName = strings.TrimSpace(modelName)
	if providerName == "" && modelName == "" {
		m := FromEnv(getenv)
		return ModelConfig{APIKey: m.APIKey, BaseURL: m.BaseURL, Model: m.Model, KeyEnv: "XBOT_API_KEY", Vision: SeesImages(m.Model, nil)}, 0, nil
	}
	if providerName == "" {
		m := FromEnv(getenv)
		m.Model = modelName
		return ModelConfig{APIKey: m.APIKey, BaseURL: m.BaseURL, Model: m.Model, KeyEnv: "XBOT_API_KEY", Vision: SeesImages(m.Model, nil)}, 0, nil
	}
	p, model, err := Lookup(root, providerName, modelName)
	if err != nil {
		return ModelConfig{}, 0, err
	}
	protocol := p.WireProtocol()
	if err := SupportedBinding(protocol, model); err != nil {
		return ModelConfig{}, 0, err
	}
	if err := validThinking(model.Thinking); err != nil {
		return ModelConfig{}, 0, err
	}
	// The file may have been edited by hand; the key goes wherever base_url says.
	if err := checkProviderBase(p.BaseURL); err != nil {
		return ModelConfig{}, 0, err
	}
	key := strings.TrimSpace(getenv(p.APIKeyEnv))
	if key == "" {
		return ModelConfig{}, 0, fmt.Errorf("providers: %s is empty", p.APIKeyEnv)
	}
	return ModelConfig{
		Protocol:     protocol,
		HostedSearch: model.HasHostedSearch(protocol),
		APIKey:       key,
		BaseURL:      strings.TrimRight(p.BaseURL, "/"),
		Model:        model.Name,
		KeyEnv:       p.APIKeyEnv,
		Thinking:     model.Thinking,
		Vision:       model.SeesImages(),
	}, model.Context, nil
}

// ModelConfig is a resolved endpoint. KeyEnv is the variable name, not a place to store the key.
// Thinking is the model spec's thinking switch, empty when unset.
// Vision is whether this model understands images.
type ModelConfig = provider.Endpoint

func validateProvider(p Provider, modelRequired bool) error {
	if err := ValidProtocol(p.WireProtocol()); err != nil {
		return err
	}
	if !nameOK(p.Name) {
		return fmt.Errorf("providers: bad name %q", p.Name)
	}
	if strings.TrimSpace(p.BaseURL) == "" {
		return errors.New("providers: base_url is required")
	}
	if err := checkProviderBase(p.BaseURL); err != nil {
		return err
	}
	if !envName.MatchString(p.APIKeyEnv) {
		return fmt.Errorf("providers: api_key_env must be an environment variable name, got %q", p.APIKeyEnv)
	}
	if modelRequired && len(p.Models) == 0 {
		return errors.New("providers: at least one model is required")
	}
	return nil
}

// checkProviderBase rejects a base_url that would leak the provider key: not
// an absolute http(s) URL, user info, a query or fragment, or plain http to a
// host that is not this machine or a private address.
func checkProviderBase(base string) error {
	base = strings.TrimSpace(base)
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return fmt.Errorf("providers: base_url %q is not an absolute URL", base)
	}
	if u.User != nil {
		return errors.New("providers: base_url must not include user info")
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return errors.New("providers: base_url must not include a query or fragment")
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return nil
	case "http":
		if localHost(u.Hostname()) {
			return nil
		}
		return fmt.Errorf("providers: base_url must be https for %s; plain http is only for this machine or a private address", u.Hostname())
	default:
		return fmt.Errorf("providers: base_url must be http or https, got %q", u.Scheme)
	}
}

// localHost is localhost, or a loopback or private IP literal.
func localHost(host string) bool {
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	return addr.IsLoopback() || addr.IsPrivate()
}

func validateModel(m ModelSpec) error {
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("providers: model name is required")
	}
	if m.Context <= 0 {
		return errors.New("providers: model context length must be positive")
	}
	if m.TodoSteps < 0 {
		return errors.New("providers: todo_steps must not be negative")
	}
	return validThinking(m.Thinking)
}

func validThinking(v string) error {
	switch v {
	case "", "off", "omit":
		return nil
	}
	return fmt.Errorf("providers: thinking must be off or omit, got %q", v)
}

func nameOK(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

func indexOf(list []Provider, name string) int {
	for i, p := range list {
		if p.Name == name {
			return i
		}
	}
	return -1
}
