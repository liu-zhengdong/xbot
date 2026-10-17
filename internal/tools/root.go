package tools

import "sync"

// rootBind is the base for relative paths. Root is the fallback used when no
// function is bound. The function is read at parse time so a session working
// directory can move between calls. A sandbox keeps its own root and does not
// bind a function.
type rootBind struct {
	mu sync.Mutex
	fn func() string
}

// BindRoot installs fn. A later call replaces it. Nil clears the hook.
func (b *rootBind) BindRoot(fn func() string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.fn = fn
	b.mu.Unlock()
}

// dir returns the bound directory, or fallback when the hook is unset or empty.
func (b *rootBind) dir(fallback string) string {
	if b == nil {
		return fallback
	}
	b.mu.Lock()
	fn := b.fn
	b.mu.Unlock()
	if fn != nil {
		if dir := fn(); dir != "" {
			return dir
		}
	}
	return fallback
}
