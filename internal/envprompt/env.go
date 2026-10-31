// Package envprompt samples external truth and renders change-only announcements.
package envprompt

import (
	"context"
	"strings"
	"sync"
)

type Value = any

type Change struct {
	Full     bool
	Previous string // last announced rendering, not a transcript-derived value
}

type Field struct {
	Key    string
	Sample func(context.Context) (Value, error)
	Prompt func(Value, Change) string
}

type Env struct {
	mu        sync.Mutex
	fields    []Field
	current   map[string]Value
	announced map[string]string
}

// Register is the only field assembly seam. Re-registering replaces a field.
func (e *Env) Register(f Field) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, old := range e.fields {
		if old.Key == f.Key {
			e.fields[i] = f
			return
		}
	}
	e.fields = append(e.fields, f)
}

// Prepare samples truth even when no announcement is due. The returned commit
// records rendered text only after the caller has successfully appended it.
// full is used when compression has removed the model's earlier context.
func (e *Env) Prepare(ctx context.Context, full bool) (string, func(), error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.current == nil {
		e.current = map[string]Value{}
	}
	rendered := map[string]string{}
	var parts []string
	for _, f := range e.fields {
		v, err := f.Sample(ctx)
		if err != nil {
			return "", nil, err
		}
		e.current[f.Key] = v
		text := f.Prompt(v, Change{Full: full, Previous: e.announced[f.Key]})
		if text != "" && (full || text != e.announced[f.Key]) {
			parts = append(parts, text)
			rendered[f.Key] = text
		}
	}
	return strings.Join(parts, "\n"), func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.announced == nil {
			e.announced = map[string]string{}
		}
		for key, text := range rendered {
			e.announced[key] = text
		}
	}, nil
}

func (e *Env) Current(key string) Value {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.current[key]
}
