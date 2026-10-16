package web

import (
	"context"
	"time"
)

// BeginTurn reserves a foreground turn. It reports false once Drain has started,
// so a restart stops taking new work without killing one already running.
func (s *Server) BeginTurn() bool {
	s.drainMu.Lock()
	defer s.drainMu.Unlock()
	if s.draining {
		return false
	}
	s.inflight++
	return true
}

// EndTurn releases a reservation made by BeginTurn.
func (s *Server) EndTurn() {
	s.drainMu.Lock()
	defer s.drainMu.Unlock()
	if s.inflight > 0 {
		s.inflight--
	}
}

// Drain stops new foreground turns and waits until the running ones end or ctx
// is done. It returns true when everyone finished, false on timeout.
func (s *Server) Drain(ctx context.Context) bool {
	s.drainMu.Lock()
	s.draining = true
	s.drainMu.Unlock()
	for {
		s.drainMu.Lock()
		left := s.inflight
		s.drainMu.Unlock()
		if left == 0 {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// Undrain re-opens the server to new turns after a cancelled restart.
func (s *Server) Undrain() {
	s.drainMu.Lock()
	s.draining = false
	s.drainMu.Unlock()
}

// Inflight reports how many foreground turns are running. Tests use it.
func (s *Server) Inflight() int {
	s.drainMu.Lock()
	defer s.drainMu.Unlock()
	return s.inflight
}
