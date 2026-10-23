package kernel

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"xbot/internal/agent"
	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/provider"
)

// A confirmation card never holds up the owner (ADR 0018). While a call waits
// on its card, an owner message parks the wait: the card stays open, the call
// returns "not run yet", and the turn answers the owner. The decision, when it
// comes, is the owner's word too: it goes straight into the running turn, or
// starts one. An approval leaves a one-time grant, so the model calls the
// tool again with the same arguments and it runs without a second card.

// grantTTL is how long an approval of a parked card stays usable.
const grantTTL = 10 * time.Minute

type grant struct {
	tool, args string
	until      time.Time
}

var (
	grantMu sync.Mutex
	grants  = map[string][]grant{} // bot dir -> grants
)

func argsKey(args json.RawMessage) string {
	var v any
	if json.Unmarshal(args, &v) == nil {
		if norm, err := json.Marshal(v); err == nil {
			args = norm
		}
	}
	sum := sha256.Sum256(args)
	return fmt.Sprintf("%x", sum[:])
}

func addGrant(b *bot.Bot, tool string, args json.RawMessage, now time.Time) {
	grantMu.Lock()
	defer grantMu.Unlock()
	key := botKey(b)
	grants[key] = append(grants[key], grant{tool: tool, args: argsKey(args), until: now.Add(grantTTL)})
}

// takeGrant uses up a grant for this exact call, if one is left.
func takeGrant(b *bot.Bot, tool string, args json.RawMessage, now time.Time) bool {
	grantMu.Lock()
	defer grantMu.Unlock()
	key := botKey(b)
	want := argsKey(args)
	list := grants[key][:0]
	found := false
	for _, g := range grants[key] {
		if now.After(g.until) {
			continue
		}
		if !found && g.tool == tool && g.args == want {
			found = true
			continue
		}
		list = append(list, g)
	}
	if len(list) == 0 {
		delete(grants, key)
	} else {
		grants[key] = list
	}
	return found
}

// parkedError is what a parked call returns; the agent shows its text as the
// tool result.
type parkedError struct{ tool string }

func (e parkedError) Error() string { return "parked: " + e.tool }

func (e parkedError) ToolResult() string {
	return "not run yet: this call is waiting on the owner's confirmation card, which stays open. The owner said something else meanwhile. Do not call it again now; you will be told when they decide."
}

// noApprover keeps the agent's wording for a link with nobody to ask.
type noApprover struct{}

func (noApprover) Error() string      { return "no approver" }
func (noApprover) ToolResult() string { return "not run: no approver available on this link" }

// parkSignal is closed when an owner message arrives, so an approval waiting
// right now parks.
func (t *turn) parkSignal() <-chan struct{} {
	t.inter.mu.Lock()
	defer t.inter.mu.Unlock()
	if t.inter.park == nil {
		t.inter.park = make(chan struct{})
	}
	return t.inter.park
}

// parkAll lets every waiting approval go. t.inter.mu must be held.
func (s *interjectState) parkAllLocked() {
	if s.park == nil {
		s.park = make(chan struct{})
	}
	if !s.parked {
		close(s.park)
		s.parked = true
	}
}

// rearmLocked gives later approvals a fresh signal, once the owner's message
// is in the conversation. t.inter.mu must be held.
func (s *interjectState) rearmLocked() {
	if s.parked {
		s.park = make(chan struct{})
		s.parked = false
	}
}

// parkable wraps the foreground approver: a granted call runs without a card,
// and a card still open when the owner speaks is parked.
func (k *Kernel) parkable(t *turn, inner agent.Approver) agent.Approver {
	return func(ctx context.Context, tool, summary string) (bool, error) {
		args := agent.ToolArgs(ctx)
		if takeGrant(k.Bot, tool, args, k.now()) {
			k.RecordDecision(bot.DecisionAllowed, tool, summary)
			return true, nil
		}
		if inner == nil {
			return false, noApprover{}
		}
		park := t.parkSignal()
		type result struct {
			ok  bool
			err error
		}
		ch := make(chan result, 1)
		go func() {
			ok, err := inner(ctx, tool, summary)
			ch <- result{ok, err}
		}()
		select {
		case r := <-ch:
			return r.ok, r.err
		case <-park:
		case <-ctx.Done():
			// A closed page must not leave the approved call tied to its
			// cancelled request. Resume it through the existing grant path.
		}
		b, sender, now := k.Bot, t.turnSender, k.now
		go func() {
			r := <-ch
			deliverDecision(b, sender, tool, args, summary, r.ok, r.err, now())
		}()
		return false, parkedError{tool: tool}
	}
}

func decisionText(tool, summary string, ok bool) string {
	if ok {
		return fmt.Sprintf("<approval_decided>\nThe owner approved the parked call: %s (%s). Call it again now with the same arguments; it runs without asking. Then finish what you were doing.\n</approval_decided>", tool, summary)
	}
	return fmt.Sprintf("<approval_decided>\nThe owner declined the parked call: %s (%s). Do not run it. Tell the owner briefly what that leaves undone.\n</approval_decided>", tool, summary)
}

// deliverDecision hands the owner's late decision on a parked card to the
// bot: into its running turn when there is one in this process, otherwise as
// an inbox event that wakes it. A card that expired with no decision wakes
// nobody.
func deliverDecision(b *bot.Bot, sender, tool string, args json.RawMessage, summary string, ok bool, err error, now time.Time) {
	if err != nil {
		return
	}
	if ok {
		addGrant(b, tool, args, now)
	}
	text := decisionText(tool, summary, ok)
	if InterjectInternal(b, provider.Message{Role: provider.RoleUser, Internal: true, Content: provider.TextContent(text)}) {
		return
	}
	box, oerr := inbox.Open(b.InboxPath())
	if oerr != nil {
		return
	}
	ev := link.NewEvent(link.NameApproval, "decision", text, sender, now)
	ev.Untrusted = false
	if box.Post(ev) != nil {
		return
	}
	if v, ok := wakers.Load(b.Root); ok {
		v.(func(*bot.Bot))(b)
	}
}

// wakers holds, per bots root, what starts a turn for an event posted outside
// any turn. The web server registers one; without it the serve loop picks the
// event up on its next pass.
var wakers sync.Map // root -> func(*bot.Bot)

// SetWaker registers fn to wake a bot under root.
func SetWaker(root string, fn func(*bot.Bot)) {
	if fn == nil {
		wakers.Delete(root)
		return
	}
	wakers.Store(root, fn)
}

// InterjectInternal hands a kernel note (an owner's late card decision) to
// the bot's running turn. It does not take over the turn's output.
func InterjectInternal(b *bot.Bot, msg provider.Message) bool {
	v, ok := liveTurns.Load(botKey(b))
	if !ok {
		return false
	}
	t := v.(*liveTurn).t
	t.inter.mu.Lock()
	if !t.inter.open {
		t.inter.mu.Unlock()
		return false
	}
	t.inter.queue = append(t.inter.queue, msg)
	t.inter.parkAllLocked()
	stop := t.inter.stop
	t.inter.mu.Unlock()
	if stop != nil {
		stop()
	}
	return true
}

// IsParked reports whether err is a parked approval.
func IsParked(err error) bool {
	var p parkedError
	return errors.As(err, &p)
}
