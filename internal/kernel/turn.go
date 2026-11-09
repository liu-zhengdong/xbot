package kernel

import (
	"context"

	"sync"
	"sync/atomic"
	"time"
	"xbot/internal/notable"
)

// turn is the state of one kernel turn. Kernel holds a pointer to the active
// turn and installs a fresh one at the start of Handle. A nil pointer means no
// turn is running; curTurn then reports zero values, exactly like the
// per-Kernel fields this replaced.
type turn struct {
	notable     notable.Log
	turnSender  string
	turnKind    string
	turnHops    int
	turnChain   string
	turnTrusted bool
	turnSource  string
	turnEvent   string
	turnDevice  string // message selection; prepared calls retain their own copy
	replyTo     int

	// useMu guards the completion tokens counted for this turn. It is not t.mu.
	useMu         sync.Mutex
	useCompletion int

	// pasteMu guards the pictures this turn has already put in a request.
	// Absorbed messages are added mid-turn, and the per-turn cap counts what the
	// whole turn carries, not what one message brought.
	pasteMu    sync.Mutex
	pasteSeen  int
	pasteBytes int

	// projects is the project instruction sets this turn's context carries.
	// A background branch keeps it: its context is this turn's.
	projects projectSigs

	// referenceProvided belongs to the turn and is used by its single model loop.
	referenceProvided map[string]bool

	// shrunk is set once this turn's requests left out or summarized one of
	// its own earlier results (see fit.go). A read receipt that points at an
	// earlier step is then no longer safe: that step may be gone.
	shrunk atomic.Bool

	// inter takes owner messages while the turn runs (interject.go).
	inter interjectState

	// mu guards reply, tool and completion state. branch preserves execution provenance.
	mu       sync.Mutex
	branch   string
	finished bool
	answered bool // a final answer is on the transcript
	// replyGen counts committed final replies. Image calls capture it when
	// issued so a continuation can attach without reviving an older call.
	replyGen int
	// Last input seen by a committed normal reply in this turn. Only used to
	// avoid false failure notices, never persisted or used for result delivery.
	replyInputThrough int
	// outImages are pictures send_message_to_user or hosted generation queued for this turn's reply line,
	// and outBytes what they weigh (outimage.go).
	outAudios []string
	outImages []string
	outBytes  int
	tools     int
	// Status tracking for the bot status feature: when the last tool call
	// started and ended, whether the turn waits on the owner, and a ring of the
	// last stepRingCap steps.
	lastToolStart   time.Time
	lastToolEnd     time.Time
	lastDescription string // last non-empty, redacted description in this turn
	awaiting        bool
	awaitingSince   time.Time
	steps           []Step
	started         time.Time
	fromSeq         int
	imageGen        *imageGenCapability
	runCtx          context.Context
	// lastRec is the sequence of the last transcript line this turn wrote.
	lastRec int
	// todoGated is set once the completion gate has continued this turn.
	todoGated bool
	// todoTouched is set when this turn wrote or updated the list. A later
	// turn does not inherit it, so an old list cannot open the gate.
	todoTouched bool
	// askedOwner is set when this turn called ask_user.
	askedOwner bool
	// todoQuiet is cached for the turn so a step does not rebuild the tool list.
	todoQuietSet bool
	todoQuietV   bool
}

func (t *turn) lastSeq() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastRec
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

// liveTurn is the turn Handle installed, or nil when none is running.
func (k *Kernel) liveTurn() *turn {
	if k == nil {
		return nil
	}
	k.turnMu.Lock()
	defer k.turnMu.Unlock()
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

// turnSeqKey carries the opening seq of the turn before the turn value itself
// exists. Handle appends the message and knows its seq before it runs the
// synchronous auto-compact, which may summarize and extract; those side calls
// must still be counted against this turn.
type turnSeqKey struct{}

func withTurnSeq(ctx context.Context, seq int) context.Context {
	return context.WithValue(ctx, turnSeqKey{}, seq)
}

// BranchOf returns the background branch id of the turn stored in ctx, or "" when
// ctx carries no turn or the turn is still on the main line. It reads the turn's
// branch under t.mu, so a fork that starts mid-call is seen.
func BranchOf(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	t, ok := ctx.Value(turnKey{}).(*turn)
	if !ok || t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.branch
}

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

// turnSeqOf is the opening transcript seq of the turn that owns ctx, or 0 when
// ctx carries no turn. Unlike turnFor it never falls back to the live turn, so
// a background job running beside a turn stays outside it.
func turnSeqOf(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	if seq, ok := ctx.Value(turnSeqKey{}).(int); ok {
		return seq
	}
	t, ok := ctx.Value(turnKey{}).(*turn)
	if !ok || t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.fromSeq
}
