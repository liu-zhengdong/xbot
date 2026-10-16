package kernel

import (
	"context"
	"sync"
	"time"
)

// turn is the state of one kernel turn. Kernel holds a pointer to the active
// turn and installs a fresh one at the start of Handle. A nil pointer means no
// turn is running; curTurn then reports zero values, exactly like the
// per-Kernel fields this replaced.
type turn struct {
	turnSender  string
	turnKind    string
	turnHops    int
	turnChain   string
	turnTrusted bool
	turnSource  string
	turnEvent   string
	replyTo     int

	notedMu sync.Mutex
	noted   []string

	// Fork state. mu guards branch, finished, tools, steps, and lastTool.
	// branch is set once, when the turn moves to the background.
	mu       sync.Mutex
	branch   string
	finished bool
	tools    int
	steps    int
	lastTool string
	started  time.Time
	fromSeq  int
	task     string
	forked   chan struct{}
	runCtx   context.Context
}

func (t *turn) isForked() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.branch != ""
}

func (t *turn) cancelled() bool {
	return t.runCtx != nil && t.runCtx.Err() != nil
}

// curTurn returns the active turn, or a zero turn when none is running, so a
// read outside a turn stays zero-valued.
func (k *Kernel) curTurn() *turn {
	if k == nil {
		return &turn{}
	}
	k.turnMu.Lock()
	defer k.turnMu.Unlock()
	if k.turn == nil {
		return &turn{}
	}
	return k.turn
}

// ensureTurn returns the active turn, creating a zero one when none is
// running, so a write outside a turn is visible to later reads.
func (k *Kernel) ensureTurn() *turn {
	k.turnMu.Lock()
	defer k.turnMu.Unlock()
	if k.turn == nil {
		k.turn = &turn{}
	}
	return k.turn
}
