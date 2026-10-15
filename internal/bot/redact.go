package bot

import "sync"

var (
	redactMu sync.Mutex
	redactFn = func(s string) string { return s }
)

// SetRedactText installs the vault replacer used on text that leaves the process.
// Export and another bot's transcript reads call RedactText. Until the vault
// installs a replacer, the text is returned unchanged. The returned function
// restores the previous replacer.
func SetRedactText(fn func(string) string) func() {
	if fn == nil {
		fn = func(s string) string { return s }
	}
	redactMu.Lock()
	prev := redactFn
	redactFn = fn
	redactMu.Unlock()
	return func() {
		redactMu.Lock()
		redactFn = prev
		redactMu.Unlock()
	}
}

// RedactText replaces known secret values. A nil installer leaves the text unchanged.
func RedactText(s string) string {
	redactMu.Lock()
	fn := redactFn
	redactMu.Unlock()
	if fn == nil {
		return s
	}
	return fn(s)
}
