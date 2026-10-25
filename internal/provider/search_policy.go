package provider

import "context"

type noHostedSearch struct{ Provider }
type noHostedSearchKey struct{}

// WithoutHostedSearch keeps the bound model and transport intact, including
// OAuth wrappers. Only the search capability is suppressed.
func WithoutHostedSearch(p Provider) Provider      { return noHostedSearch{p} }
func (p noHostedSearch) HostedSearchEnabled() bool { return false }
func (p noHostedSearch) UnwrapProvider() Provider  { return p.Provider }
func (p noHostedSearch) Chat(ctx context.Context, req Request, delta func(string)) (Message, error) {
	req.HostedSearch = false
	return p.Provider.Chat(context.WithValue(ctx, noHostedSearchKey{}, true), req, delta)
}
func hostedSearchAllowed(ctx context.Context) bool {
	disabled, _ := ctx.Value(noHostedSearchKey{}).(bool)
	return !disabled
}
