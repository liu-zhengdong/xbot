// Package promptinject assembles tail prompt sources without touching the stable prefix.
package promptinject

import (
	"context"
	"sort"
)

type Source struct {
	Name   string
	Order  int
	Render func(context.Context) (string, error)
}

type Registry struct{ sources []Source }

// Register adds or replaces a source. Register sources before rendering.
func (r *Registry) Register(s Source) {
	for i, old := range r.sources {
		if old.Name == s.Name {
			r.sources[i] = s
			return
		}
	}
	r.sources = append(r.sources, s)
}

// Render omits empty sources and preserves registration order for equal Order.
func (r *Registry) Render(ctx context.Context) ([]string, error) {
	sources := append([]Source(nil), r.sources...)
	sort.SliceStable(sources, func(i, j int) bool { return sources[i].Order < sources[j].Order })
	var out []string
	for _, s := range sources {
		text, err := s.Render(ctx)
		if err != nil {
			return nil, err
		}
		if text != "" {
			out = append(out, text)
		}
	}
	return out, nil
}
