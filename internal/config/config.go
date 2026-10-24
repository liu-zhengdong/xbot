// Package config reads process environment for the model provider.
package config

import "strings"

const (
	defaultBaseURL = "https://api.deepseek.com/v1"
	defaultModel   = "deepseek-flash"
)

// Model is the OpenAI-compatible endpoint the bot talks to.
type Model struct {
	APIKey  string
	BaseURL string
	Model   string
}

// FromEnv reads XBOT_API_KEY, XBOT_BASE_URL, and XBOT_MODEL.
func FromEnv(getenv func(string) string) Model {
	m := Model{
		APIKey:  strings.TrimSpace(getenv("XBOT_API_KEY")),
		BaseURL: strings.TrimRight(strings.TrimSpace(getenv("XBOT_BASE_URL")), "/"),
		Model:   strings.TrimSpace(getenv("XBOT_MODEL")),
	}
	if m.BaseURL == "" {
		m.BaseURL = defaultBaseURL
	}
	if m.Model == "" {
		m.Model = defaultModel
	}
	return m
}

// SeesImages reports whether a model understands images.
// An explicit spec flag wins. Otherwise deepseek-flash does (including a
// routed id whose name contains both "deepseek" and "flash"), and
// deepseek-chat and deepseek-v4-pro do not. An empty name is the product
// default, deepseek-flash. Known OpenAI and Claude models also support images;
// other ids require an explicit vision flag.
func SeesImages(name string, explicit *bool) bool {
	if explicit != nil {
		return *explicit
	}
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		n = defaultModel
	}
	if strings.Contains(n, "deepseek-v4-pro") {
		return false
	}
	if strings.Contains(n, "deepseek-chat") {
		return false
	}
	if openAIImageAndSearch(n) {
		return true
	}
	switch n {
	case "claude-sonnet-4-5", "claude-opus-4-6":
		return true
	}
	return strings.Contains(n, "deepseek") && strings.Contains(n, "flash")
}

// SeesImages reports this spec's image capability.
func (m ModelSpec) SeesImages() bool { return SeesImages(m.Name, m.Vision) }
