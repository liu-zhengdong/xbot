package mockopenai

import (
	"context"
	"sync"
)

// Script records request bodies and blocks selected mock steps until Release.
type Script struct {
	mu     sync.Mutex
	bodies []string
	gates  map[string]*gate
}

type gate struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

// NewScript returns an empty script.
func NewScript() *Script {
	return &Script{gates: map[string]*gate{}}
}

func (s *Script) add(body string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.bodies = append(s.bodies, body)
	s.mu.Unlock()
}

// Bodies returns a copy of the recorded request bodies.
func (s *Script) Bodies() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bodies...)
}

func (s *Script) gate(name string) *gate {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.gates[name]
	if !ok {
		g = &gate{entered: make(chan struct{}), release: make(chan struct{})}
		s.gates[name] = g
	}
	return g
}

// Enter marks that a held step has started. It is safe to call once.
func (s *Script) Enter(name string) {
	g := s.gate(name)
	g.once.Do(func() { close(g.entered) })
}

// WaitEntered blocks until Enter(name) or ctx is done.
func (s *Script) WaitEntered(ctx context.Context, name string) error {
	g := s.gate(name)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-g.entered:
		return nil
	}
}

// Wait blocks until Release or ctx is done.
func (s *Script) Wait(name string, ctx context.Context) error {
	g := s.gate(name)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-g.release:
		return nil
	}
}

// Release unblocks Wait. A second call does nothing.
func (s *Script) Release(name string) {
	g := s.gate(name)
	select {
	case <-g.release:
	default:
		close(g.release)
	}
}
