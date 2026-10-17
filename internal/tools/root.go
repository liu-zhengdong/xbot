package tools

import (
	"context"
	"sync"
)

// rootBind is the base for relative paths. Root is the fallback used when no
// function is bound. The function is read at parse time, with the call
// context, so each execution context can keep its own directory. A sandbox
// keeps its own root and does not bind a function.
type rootBind struct {
	mu sync.Mutex
	fn func(context.Context) string
}

// BindRoot installs fn. A later call replaces it. Nil clears the hook.
func (b *rootBind) BindRoot(fn func(context.Context) string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.fn = fn
	b.mu.Unlock()
}

// dir returns the bound directory, or fallback when the hook is unset or empty.
func (b *rootBind) dir(ctx context.Context, fallback string) string {
	if b == nil {
		return fallback
	}
	b.mu.Lock()
	fn := b.fn
	b.mu.Unlock()
	if fn != nil {
		if dir := fn(ctx); dir != "" {
			return dir
		}
	}
	return fallback
}
