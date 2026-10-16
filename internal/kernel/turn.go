package kernel

import (
	"context"

	"sync"
	"time"
	"xbot/internal/provider"
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
	answered bool // a final answer is on the transcript; too late to fork
	// forkMu serializes the fork decision with absorbing the inbox, so a message
	// absorbed into this turn cannot also be the one that forks it.
	forkMu    sync.Mutex
	tools     int
	steps     int
	lastTool  string
	started   time.Time
	fromSeq   int
	task      string
	forked    chan struct{}
	runCtx    context.Context
	cancelRun context.CancelFunc
	// ctrl is controls sent to a background, read between its steps. resources is
	// what this turn has written (review.ResourceOf), for the conflict queue.
	ctrl      []*Control
	resources map[string]bool
}

// queueCtrl adds a control message. It fails once the turn has finished.
func (t *turn) queueCtrl(c *Control) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.finished {
		return false
	}
	c.Background = t.branch
	c.State = "pending"
	t.ctrl = append(t.ctrl, c)
	return true
}

// takeCtrl marks every pending control applied and returns them as user messages.
// It runs between steps: after the current tool call returned, before the next
// model call.
func (t *turn) takeCtrl() []provider.Message {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.takeCtrlLocked()
}

func (t *turn) takeCtrlLocked() []provider.Message {
	var out []provider.Message
	for _, c := range t.ctrl {
		if c.State == "pending" {
			c.State = "applied"
			out = append(out, provider.Message{Role: provider.RoleUser, Content: c.message()})
		}
	}
	return out
}

func (t *turn) addResource(res string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.resources == nil {
		t.resources = map[string]bool{}
	}
	t.resources[res] = true
}

func (t *turn) touches(res string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.resources[res]
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

type turnKey struct{}

// turnFor returns the turn that owns ctx. Tool code runs on its turn's context, so a
// background turn's tools see their own turn even after k.turn moved to the foreground.
func (k *Kernel) turnFor(ctx context.Context) *turn {
	if ctx != nil {
		if t, ok := ctx.Value(turnKey{}).(*turn); ok && t != nil {
			return t
		}
	}
	return k.ensureTurn()
}
