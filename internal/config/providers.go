package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"xbot/internal/compact"
	"xbot/internal/fsutil"
	"xbot/internal/provider"
	"xbot/internal/vault"
)

// envName is an environment variable name, never a secret.
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Provider is one protocol endpoint. The key is the name of an environment variable.
type Provider struct {
	Name        string      `json:"name"`
	DisplayName string      `json:"display_name,omitempty"`
	Custom      bool        `json:"custom,omitempty"`
	Protocol    string      `json:"protocol,omitempty"`
	BaseURL     string      `json:"base_url"`
	APIKeyEnv   string      `json:"api_key_env"`
	OAuth       string      `json:"oauth,omitempty"`
	Models      []ModelSpec `json:"models"`
}

// ModelSpec is one model id and its context length in tokens.
// Todo turns the todo reminder on or off. Nil means the default: on when the
// provider or model name contains "deepseek", off otherwise.
// TodoSteps is how many tool steps a list must sit unchanged before a reminder.
// Zero means the default of 6.
// Thinking is "on" (send thinking enabled and replay the model's reasoning),
// "off" (send thinking disabled), "omit" (send no thinking field), or empty:
// on for DeepSeek model names, omit for the rest.
// ReasoningEffort is low/high/max; empty defaults to low on thinking requests.
// Vision, when set, is this model's image understanding. Nil uses the default
// for the model id: deepseek-flash sees images; deepseek-chat and
// deepseek-v4-pro do not. Other ids do not, until the spec says so.
// HostedImageGen mounts the vendor's image_generation tool on a Responses
// binding; nil and false leave it off. It is independent of HostedSearch.
// ImageGenModel is the OpenAI tool-level image model; empty uses the vendor
// default, and xAI ignores it.
type ModelSpec struct {
	Name            string `json:"name"`
	Context         int    `json:"context"`
	Todo            *bool  `json:"todo,omitempty"`
	TodoSteps       int    `json:"todo_steps,omitempty"`
	Thinking        string `json:"thinking,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	Vision          *bool  `json:"vision,omitempty"`
	HostedSearch    *bool  `json:"hosted_web_search,omitempty"`

	HostedImageGen *bool  `json:"hosted_image_generation,omitempty"`
	ImageGenModel  string `json:"image_generation_model,omitempty"`
}

// ContextWindow uses the catalog value, falling back only when it is absent.
func (m ModelSpec) ContextWindow() int {
	if m.Context > 0 {
		return m.Context
	}
	return compact.DefaultWindow
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
	for _, p := range SearchProviders() {
		if p.NeedsKey {
			add(SearchSecretName(p.ID))
		}
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

// Add upserts an endpoint and optional models. With no models it registers an
// endpoint for catalog discovery; existing models and capabilities are kept.
func Add(root string, p Provider, models ...ModelSpec) error {
	return fsutil.WithLock(Path(root)+".lock", func() error { return add(root, p, models...) })
}

func add(root string, p Provider, models ...ModelSpec) error {
	if !nameOK(p.Name) {
		return fmt.Errorf("providers: bad name %q", p.Name)
	}
	for _, model := range models {
		if err := validateModel(model); err != nil {
			return err
		}
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
		p.Models = append([]ModelSpec{}, models...)
		f.Providers = append(f.Providers, p)
		return Save(root, f)
	}
	cur := f.Providers[i]
	if p.Custom {
		cur.Custom = true
		cur.DisplayName = p.DisplayName
	}
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
		cur.OAuth = ""
	}
	if p.OAuth != "" {
		cur.OAuth = p.OAuth
		cur.APIKeyEnv = ""
	}
	for _, model := range models {
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
				if model.ReasoningEffort == "" {
					model.ReasoningEffort = cur.Models[n].ReasoningEffort
				}
				if model.Vision == nil {
					model.Vision = cur.Models[n].Vision
				}
				if model.HostedSearch == nil {
					model.HostedSearch = cur.Models[n].HostedSearch
				}
				if model.HostedImageGen == nil {
					model.HostedImageGen = cur.Models[n].HostedImageGen
					model.ImageGenModel = cur.Models[n].ImageGenModel
				}
				cur.Models[n] = model
				replaced = true
				break
			}
		}
		if !replaced {
			cur.Models = append(cur.Models, model)
		}
	}
	f.Providers[i] = cur
	return Save(root, f)
}

// Remove deletes a provider, or one model when model is not empty.
// Removing the last model deletes the provider.
func Remove(root, name, model string) error {
	return fsutil.WithLock(Path(root)+".lock", func() error { return remove(root, name, model) })
}

func remove(root, name, model string) error {
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
	if len(next) == 0 && !IsCustomProvider(cur) {
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

// SetImageGen switches vendor-hosted image generation on one registered model
// and sets its OpenAI tool-level image model. Only Responses bindings take it.
// The image model is kept as given, trimmed; empty clears it.
func SetImageGen(root, providerName, modelName string, on bool, imageModel string) error {
	imageModel = strings.TrimSpace(imageModel)
	if len(imageModel) > 128 || strings.ContainsAny(imageModel, " \t\r\n\"") {
		return errors.New("providers: bad image_generation_model")
	}
	return fsutil.WithLock(Path(root)+".lock", func() error {
		f, err := Load(root)
		if err != nil {
			return err
		}
		i := indexOf(f.Providers, providerName)
		if i < 0 {
			return fmt.Errorf("providers: %q not found", providerName)
		}
		p := &f.Providers[i]
		for n := range p.Models {
			if p.Models[n].Name != modelName {
				continue
			}
			if on && p.WireProtocol() != Responses {
				return errors.New("providers: hosted_image_generation requires responses")
			}
			p.Models[n].HostedImageGen = &on
			p.Models[n].ImageGenModel = imageModel
			return Save(root, f)
		}
		return fmt.Errorf("providers: model %q is not on %q", modelName, providerName)
	})
}

// DefaultProvider is the registry entry new bots bind to and bots saved
// without a provider move to. It is an ordinary entry in providers.json.
const DefaultProvider = "deepseek"

// EnsureDefault returns the deepseek entry, first adding it when the registry
// lacks it and adding each given model it does not list yet. A new entry
// takes XBOT_BASE_URL and XBOT_MODEL (defaults https://api.deepseek.com/v1
// and deepseek-flash) and api_key_env XBOT_API_KEY, so an upgrade keeps the
// endpoint the environment used to give. Nothing is written when nothing is
// missing.
func EnsureDefault(root string, models ...string) (Provider, error) {
	var out Provider
	err := fsutil.WithLock(Path(root)+".lock", func() error {
		f, err := Load(root)
		if err != nil {
			return err
		}
		changed := false
		i := indexOf(f.Providers, DefaultProvider)
		if i < 0 {
			m := FromEnv(Getenv)
			f.Providers = append(f.Providers, Provider{Name: DefaultProvider, BaseURL: m.BaseURL, APIKeyEnv: "XBOT_API_KEY", Models: []ModelSpec{{Name: m.Model, Context: compact.DefaultWindow}}})
			i = len(f.Providers) - 1
			changed = true
		}
		p := &f.Providers[i]
		for _, name := range models {
			if name = strings.TrimSpace(name); name == "" {
				continue
			}
			if _, ok := registeredSpec(*p, name); !ok {
				p.Models = append(p.Models, ModelSpec{Name: name, Context: compact.DefaultWindow})
				changed = true
			}
		}
		out = *p
		if !changed {
			return nil
		}
		return Save(root, f)
	})
	return out, err
}

// Resolve turns a bot binding into an endpoint. Every binding names a
// registry entry; the API key is read from the environment variable named there.
func Resolve(root, providerName, modelName string, getenv func(string) string) (endpoint ModelConfig, contextLen int, err error) {
	providerName = strings.TrimSpace(providerName)
	modelName = strings.TrimSpace(modelName)
	if providerName == "" {
		return ModelConfig{}, 0, errors.New("providers: no provider bound")
	}
	p, model, err := Lookup(root, providerName, modelName)
	if err != nil {
		return ModelConfig{}, 0, err
	}
	return resolveProvider(root, p, model, getenv)
}

func resolveProvider(root string, p Provider, model ModelSpec, getenv func(string) string) (ModelConfig, int, error) {
	protocol := p.WireProtocol()
	if err := SupportedBinding(protocol, model); err != nil {
		return ModelConfig{}, 0, err
	}
	if err := validThinking(model.Thinking); err != nil {
		return ModelConfig{}, 0, err
	}
	if err := validReasoningEffort(model.ReasoningEffort); err != nil {
		return ModelConfig{}, 0, err
	}
	// The file may have been edited by hand; the key goes wherever base_url says.
	if err := checkProviderBase(p.BaseURL); err != nil {
		return ModelConfig{}, 0, err
	}
	key := strings.TrimSpace(getenv(p.APIKeyEnv))
	var oauthHTTP *http.Client
	if p.OAuth != "" {
		if err := validateOAuth(p); err != nil {
			return ModelConfig{}, 0, err
		}
		backend, err := OAuthVault(root)
		if err != nil {
			return ModelConfig{}, 0, err
		}
		info, err := vault.OAuthMetadata(context.Background(), backend, p.OAuth)
		if err != nil {
			return ModelConfig{}, 0, err
		}
		if !info.Configured {
			return ModelConfig{}, 0, fmt.Errorf("providers: %s login required", p.OAuth)
		}
		key = "{{secret:" + p.OAuth + "}}"
		oauthHTTP = &http.Client{Transport: vault.OAuthTransport(backend, p.OAuth), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	if key == "" {
		return ModelConfig{}, 0, fmt.Errorf("providers: %s is empty", p.APIKeyEnv)
	}
	return ModelConfig{
		Protocol:        protocol,
		HostedSearch:    model.HasHostedSearch(protocol),
		APIKey:          key,
		BaseURL:         strings.TrimRight(p.BaseURL, "/"),
		Model:           model.Name,
		KeyEnv:          p.APIKeyEnv,
		Thinking:        model.Thinking,
		ReasoningEffort: model.ReasoningEffort,
		Vision:          model.SeesImages(),
		HTTPClient:      oauthHTTP,
	}, model.ContextWindow(), nil
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
	if p.OAuth != "" {
		if err := validateOAuth(p); err != nil {
			return err
		}
	} else if !envName.MatchString(p.APIKeyEnv) {
		return fmt.Errorf("providers: api_key_env must be an environment variable name, got %q", p.APIKeyEnv)
	}
	if modelRequired && len(p.Models) == 0 {
		return errors.New("providers: at least one model is required")
	}
	return nil
}

// checkProviderBase accepts an explicit http(s) endpoint without credentials,
// query or fragment. Settings warn about HTTP but permit it (ADR 0037).
func checkProviderBase(base string) error {
	base = strings.TrimSpace(base)
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return errors.New("providers: Base URL 必须是完整的 http(s) 地址")
	}
	if u.User != nil {
		return errors.New("providers: base_url must not include user info")
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return errors.New("providers: base_url must not include a query or fragment")
	}
	switch strings.ToLower(u.Scheme) {
	case "https", "http":
		return nil
	default:
		return fmt.Errorf("providers: base_url must be http or https, got %q", u.Scheme)
	}
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
	if err := validThinking(m.Thinking); err != nil {
		return err
	}
	return validReasoningEffort(m.ReasoningEffort)
}

func validThinking(v string) error {
	switch v {
	case "", "on", "off", "omit":
		return nil
	}
	return fmt.Errorf("providers: thinking must be on, off or omit, got %q", v)
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

func validReasoningEffort(v string) error {
	switch v {
	case "", "low", "high", "max":
		return nil
	}
	return fmt.Errorf("providers: reasoning_effort must be low, high or max, got %q", v)
}
