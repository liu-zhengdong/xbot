// Package deviceidentity carries identities fixed by authenticated device transport.
package deviceidentity

import "context"

type Identity struct {
	AccountKey      string `json:"account_key"`
	DeviceID        string `json:"device_id"`
	BindingID       string `json:"binding_id"`
	TargetBindingID string `json:"target_binding_id"`
	Bot             string `json:"bot"`
	ContextID       string `json:"context_id"`
	Readonly        bool   `json:"readonly"`
	Owner           bool   `json:"owner,omitempty"`
}
type consoleKey struct{}

// WithConsoleSession carries only the account session of a user console ticket.
func WithConsoleSession(ctx context.Context, session string) context.Context {
	return context.WithValue(ctx, consoleKey{}, session)
}
func ConsoleSession(ctx context.Context) string { v, _ := ctx.Value(consoleKey{}).(string); return v }

type sourceKey struct{}

func WithSource(ctx context.Context, source Identity) context.Context {
	return context.WithValue(ctx, sourceKey{}, source)
}
func Source(ctx context.Context) (Identity, bool) {
	v, ok := ctx.Value(sourceKey{}).(Identity)
	return v, ok
}
