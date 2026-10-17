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
