package config

import (
	"fmt"
	"time"

	"xbot/internal/provider"
)

// ModelSelector assembles quota policy once, beside the normal model binding.
// Backup resolution deliberately does not recurse into another fallback policy.
func ModelSelector(root, providerName string, primary provider.Provider, getenv func(string) string) (provider.ModelSelector, error) {
	f, err := Load(root)
	if err != nil {
		return nil, err
	}
	i := indexOf(f.Providers, providerName)
	if i < 0 {
		return nil, fmt.Errorf("providers: unknown provider %q", providerName)
	}
	p := f.Providers[i]
	if p.RateLimitWaitSeconds < 0 {
		return nil, fmt.Errorf("providers: rate_limit_wait_seconds must be nonnegative")
	}
	s := provider.RateLimitSelector{Primary: provider.FixedSelector{P: primary}, MaxWait: time.Duration(p.RateLimitWaitSeconds) * time.Second}
	if p.Fallback != nil {
		if p.Fallback.Provider == "" || p.Fallback.Model == "" {
			return nil, fmt.Errorf("providers: fallback requires provider and model")
		}
		endpoint, _, err := Resolve(root, p.Fallback.Provider, p.Fallback.Model, getenv)
		if err != nil {
			return nil, fmt.Errorf("providers: fallback: %w", err)
		}
		backup, err := provider.New(endpoint)
		if err != nil {
			return nil, err
		}
		s.Fallback = provider.FixedSelector{P: backup}
	}
	return s, nil
}
