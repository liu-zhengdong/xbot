package web

import (
	"context"
	"testing"
	"time"
)

func TestDrainBlocksNewTurnsAndWaits(t *testing.T) {
	s := &Server{}
	if !s.BeginTurn() {
		t.Fatal("first turn refused")
	}
	done := make(chan bool, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		done <- s.Drain(ctx)
	}()
	// Wait for Drain to set the flag before probing BeginTurn.
	deadline := time.Now().Add(time.Second)
	for {
		s.drainMu.Lock()
		draining := s.draining
		s.drainMu.Unlock()
		if draining {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Drain did not flag draining")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if s.BeginTurn() {
		t.Fatal("BeginTurn admitted a turn while draining")
	}
	s.EndTurn()
	select {
	case finished := <-done:
		if !finished {
			t.Fatal("Drain reported timeout")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Drain did not return")
	}
}

func TestDrainTimesOut(t *testing.T) {
	s := &Server{}
	s.BeginTurn()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if s.Drain(ctx) {
		t.Fatal("Drain reported success with an in-flight turn")
	}
	s.EndTurn()
	s.Undrain()
	if !s.BeginTurn() {
		t.Fatal("Undrain did not reopen the server")
	}
}
