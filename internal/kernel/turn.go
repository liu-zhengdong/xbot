package kernel

import "sync"

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
}

// curTurn returns the active turn, or a zero turn when none is running, so a
// read outside a turn stays zero-valued.
func (k *Kernel) curTurn() *turn {
	if k == nil || k.turn == nil {
		return &turn{}
	}
	return k.turn
}

// ensureTurn returns the active turn, creating a zero one when none is
// running, so a write outside a turn is visible to later reads.
func (k *Kernel) ensureTurn() *turn {
	if k.turn == nil {
		k.turn = &turn{}
	}
	return k.turn
}
