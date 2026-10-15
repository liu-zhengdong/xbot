package config

import (
	"os"
	"strings"
	"sync"
)

var (
	heldMu    sync.Mutex
	heldKeys  map[string]string
	heldNames map[string]bool
)

// HoldKeys copies the named variables into memory and removes them from the
// process environment. Later Getenv and Held still return the values. Children
// cannot read them back from /proc/<pid>/environ.
func HoldKeys(names []string) {
	heldMu.Lock()
	defer heldMu.Unlock()
	if heldKeys == nil {
		heldKeys = map[string]string{}
	}
	if heldNames == nil {
		heldNames = map[string]bool{}
	}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		heldNames[name] = true
		value, ok := os.LookupEnv(name)
		if !ok {
			continue
		}
		value = strings.Clone(value)
		heldKeys[name] = value
		// Unsetenv drops the variable from later children. The bytes passed
		// at exec stay readable in /proc until they are overwritten.
		scrubEnviron(name, value)
		_ = os.Unsetenv(name)
	}
}

// ForgetKeys drops the in-memory copies. Tests use it.
func ForgetKeys() {
	heldMu.Lock()
	heldKeys = nil
	heldNames = nil
	heldMu.Unlock()
}

// ProviderKey reports a name passed to HoldKeys. Those values stay in memory
// and must not be copied into a child process or an outbound header.
func ProviderKey(name string) bool {
	heldMu.Lock()
	defer heldMu.Unlock()
	return heldNames[strings.TrimSpace(name)]
}

// Getenv returns a held provider key, or the process environment.
func Getenv(key string) string {
	if value, ok := Held(key); ok {
		return value
	}
	return os.Getenv(key)
}

// Held reports a provider key kept in memory after HoldKeys.
func Held(key string) (string, bool) {
	heldMu.Lock()
	defer heldMu.Unlock()
	value, ok := heldKeys[key]
	return value, ok
}
