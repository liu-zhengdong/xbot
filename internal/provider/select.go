package provider

import (
	"context"
	"errors"
)

// Hint is what a selector may look at. FixedSelector ignores it.
// A future auto router can read the bot name and, later, the wake itself.
type Hint struct {
	Bot string
}

// ModelSelector picks the provider for one wake.
// The kernel depends on this seam, not on a routing policy.
type ModelSelector interface {
	Select(ctx context.Context, hint Hint) (Provider, error)
}

// FixedSelector always returns the bot's bound provider. It does not route.
type FixedSelector struct {
	P Provider
}

// Select returns the bound provider.
func (f FixedSelector) Select(context.Context, Hint) (Provider, error) {
	if f.P == nil {
		return nil, errors.New("provider: no model is bound")
	}
	return f.P, nil
}
