package config

import (
	"os"
	"sort"
	"strings"
	"sync"
)

var (
	heldMu       sync.Mutex
	heldKeys     map[string]string
	heldNames    map[string]bool
	heldRedact   []string
	heldRedactOK bool
	heldRepl     *strings.Replacer // one pass over heldRedact
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
	heldRedactOK = false
	heldRedact = nil
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

// SetHeld keeps a provider key that arrived after startup, such as the one the
// desktop first-run screen saves. It is never put in the process environment.
func SetHeld(name, value string) {
	heldMu.Lock()
	defer heldMu.Unlock()
	if heldKeys == nil {
		heldKeys = map[string]string{}
	}
	if heldNames == nil {
		heldNames = map[string]bool{}
	}
	heldNames[name] = true
	heldKeys[name] = strings.Clone(value)
	heldRedactOK = false
	heldRedact = nil
}

// ForgetKeys drops the in-memory copies. Tests use it.
func ForgetKeys() {
	heldMu.Lock()
	heldKeys = nil
	heldNames = nil
	heldRedact = nil
	heldRedactOK = false
	heldMu.Unlock()
}

// ProviderKey reports a name passed to HoldKeys. Those values stay in memory
// and must not be copied into a child process or an outbound header.
func ProviderKey(name string) bool {
	heldMu.Lock()
	defer heldMu.Unlock()
	return heldNames[strings.TrimSpace(name)]
}

// RedactHeld replaces held key values with [secret]. Values shorter than 8
// bytes are left alone so a short test string cannot blank unrelated text.
// Keys held after startup are included, because the lookup reads the map.
// The sorted value list is cached until HoldKeys or ForgetKeys. Callers must
// not mutate the slice RedactHeld reads; HoldKeys replaces it.
func RedactHeld(s string) string {
	if s == "" {
		return s
	}
	heldMu.Lock()
	redactListLocked()
	repl := heldRepl
	heldMu.Unlock()
	if repl == nil {
		return s
	}
	// One pass, longest first at each position: an inserted [secret] is not
	// scanned again.
	return repl.Replace(s)
}

// HeldForms returns a copy of the values RedactHeld would replace, longest
// first. Shell output uses it to hold back a key split across writes.
func HeldForms() []string {
	heldMu.Lock()
	defer heldMu.Unlock()
	src := redactListLocked()
	if len(src) == 0 {
		return nil
	}
	out := make([]string, len(src))
	copy(out, src)
	return out
}

func redactListLocked() []string {
	if heldRedactOK {
		return heldRedact
	}
	vals := make([]string, 0, len(heldKeys))
	for _, v := range heldKeys {
		if len(v) >= 8 {
			vals = append(vals, v)
		}
	}
	sort.Slice(vals, func(i, j int) bool {
		if len(vals[i]) != len(vals[j]) {
			return len(vals[i]) > len(vals[j])
		}
		return vals[i] < vals[j]
	})
	heldRedact = vals
	heldRepl = nil
	if len(vals) > 0 {
		pairs := make([]string, 0, 2*len(vals))
		for _, v := range vals {
			pairs = append(pairs, v, "[secret]")
		}
		heldRepl = strings.NewReplacer(pairs...)
	}
	heldRedactOK = true
	return heldRedact
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

// HeldEnv merges the held provider keys back into env as NAME=value entries,
// replacing any existing entry. Only a self re-exec uses it, so the upgraded
// process can hold the keys again; no child tool ever gets this env.
func HeldEnv(env []string) []string {
	heldMu.Lock()
	defer heldMu.Unlock()
	out := make([]string, 0, len(env)+len(heldKeys))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if _, held := heldKeys[name]; !held {
			out = append(out, kv)
		}
	}
	for name, value := range heldKeys {
		out = append(out, name+"="+value)
	}
	return out
}
