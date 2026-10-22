package kernel

import (
	"context"
	"strings"
	"sync"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
)

// Owner words go straight into the running turn (ADR 0017). A main-line turn
// registers itself here for its bot while it runs; a web message for that bot
// is handed to it instead of the inbox. A model call in flight is stopped and
// sent again with the message; a tool in flight finishes first.

// Sink is where a turn's live output goes. The newest owner message takes it
// over: deltas, tool lines, and the end of the turn go to whoever spoke last.
type Sink struct {
	OnDelta  func(string)
	OnTool   func(name, summary string)
	OnResult func(name, output string)
	// OnHandoff runs once, when a newer owner message takes the output. The
	// request behind this sink can then end its stream: the answer goes to
	// the newer one.
	OnHandoff func()
	// Done receives the outcome when the turn ends, unless the output was
	// handed off first. It must have room for one value.
	Done chan Outcome
}

// Outcome is how a turn ended, for the sink that held its output last.
type Outcome struct {
	Err error
	// ReplyTo is the user line the final answer pointed at, or 0.
	ReplyTo int
}

// liveTurns maps a bot's directory to its running main-line turn.
var liveTurns sync.Map // string -> *liveTurn

type liveTurn struct {
	k *Kernel
	t *turn
}

func botKey(b *bot.Bot) string {
	if b == nil {
		return ""
	}
	return bot.Dir(b.Root, b.Name())
}

// interjectState is the part of a turn that takes owner messages.
type interjectState struct {
	mu     sync.Mutex
	open   bool
	queue  []provider.Message
	stop   context.CancelFunc // the model call in flight, or nil
	sink   *Sink
	first  *Sink // the Handle caller's
	handed bool  // the Handle caller's output was taken over
	// park is closed when an owner message arrives, so a confirmation card
	// being waited on parks (park.go). parked says it is closed.
	park   chan struct{}
	parked bool
}

// openInterject routes t's output to first and registers t for its bot. A
// kernel that forks turns does not register: interjections fork as before.
func (k *Kernel) openInterject(t *turn, first *Sink) {
	t.inter.mu.Lock()
	t.inter.open = true
	t.inter.sink = first
	t.inter.first = first
	t.inter.mu.Unlock()
	if k.AllowFork {
		return
	}
	liveTurns.Store(botKey(k.Bot), &liveTurn{k: k, t: t})
}

// closeInterject ends interjection for t. Unless force is set, messages
// still queued keep it open and are returned, for the caller to run another
// segment with. With force it closes; those inputs are already durable.
func (k *Kernel) closeInterject(t *turn, force bool) []provider.Message {
	t.inter.mu.Lock()
	defer t.inter.mu.Unlock()
	q := t.inter.queue
	t.inter.queue = nil
	if len(q) > 0 && !force {
		t.inter.rearmLocked()
		return q
	}
	if !t.inter.open {
		return nil
	}
	t.inter.open = false
	if v, ok := liveTurns.Load(botKey(k.Bot)); ok && v.(*liveTurn).t == t {
		liveTurns.Delete(botKey(k.Bot))
	}
	return q
}

// receive appends a normal input once, before setup or model cancellation.
// Queue entries carry its sequence; replaying a model request never appends it again.
func (k *Kernel) receive(ev link.Event) (int, error) {
	seq, err := k.Bot.FindEvent(ev.ID)
	if err != nil {
		return 0, err
	}
	if seq == 0 {
		origin := bot.OriginOwner
		if ev.Source == link.NameApproval {
			origin = bot.OriginInternal
		}
		seq, err = k.Bot.AppendSeq(bot.Entry{Time: ev.Time, EventID: ev.ID, ReplyTo: ev.ReplyTo, Origin: origin, Images: ev.Images,
			Message: provider.Message{Role: provider.RoleUser, Content: link.Render(ev, link.OwnerTrusted(ev.Source, ev.Sender, k.Bot.Config().Owners))}})
		if err != nil {
			return 0, err
		}
	}
	return seq, k.receiveEnvironment(ev, seq)
}

// Interject hands an owner message to the bot's running turn. It returns
// false when no turn of this process is running for the bot; the caller then
// starts a turn itself. On true, sink holds the turn's output from now on.
func Interject(b *bot.Bot, ev link.Event, sink *Sink) bool {
	v, ok := liveTurns.Load(botKey(b))
	if !ok {
		return false
	}
	lt := v.(*liveTurn)
	if link.OwnerTrusted(ev.Source, ev.Sender, b.Config().Owners) {
		AnswerQuestions(b, ev.Content)
	}
	msg := lt.k.eventMessage(ev)
	t := lt.t
	t.inter.mu.Lock()
	if !t.inter.open {
		t.inter.mu.Unlock()
		return false
	}
	seq, err := lt.k.receive(ev)
	if err != nil {
		t.inter.mu.Unlock()
		return false
	}
	msg.Seq = seq
	t.inter.queue = append(t.inter.queue, msg, interjectNote())
	prev := t.inter.sink
	t.inter.sink = sink
	if prev != nil && prev == t.inter.first {
		t.inter.handed = true
	}
	t.inter.parkAllLocked()
	stop := t.inter.stop
	t.inter.mu.Unlock()
	if prev != nil && prev.OnHandoff != nil {
		prev.OnHandoff()
	}
	if stop != nil {
		stop()
	}
	return true
}

// interjectNoteText follows an owner message taken in mid-turn. Without it
// the model reads the newest line as the whole request and drops what it was
// doing (real DeepSeek, 2026-10-20: asked the time mid-task, it answered the
// time and ended the turn).
const interjectNoteText = "<owner_interjected>\nThe owner said this while you were in the middle of something. Address all outstanding owner requests in this conversation, including the new words. A newer message does not replace earlier unfinished requests unless the owner changes or cancels them. Keep already dispatched work running; do not dispatch it again.\n</owner_interjected>"

func interjectNote() provider.Message {
	return provider.Message{Role: provider.RoleUser, Content: interjectNoteText, Internal: true}
}

// takeInterjected returns the queued owner messages, oldest first.
func (t *turn) takeInterjected() []provider.Message {
	t.inter.mu.Lock()
	defer t.inter.mu.Unlock()
	q := t.inter.queue
	t.inter.queue = nil
	if len(q) > 0 {
		t.inter.rearmLocked()
	}
	return q
}

// preempt records the cancel of the model call going out. A message already
// queued stops it at once.
func (t *turn) preempt(stop context.CancelFunc) func() {
	t.inter.mu.Lock()
	t.inter.stop = stop
	queued := len(t.inter.queue) > 0
	t.inter.mu.Unlock()
	if queued {
		stop()
	}
	return func() {
		t.inter.mu.Lock()
		t.inter.stop = nil
		t.inter.mu.Unlock()
	}
}

func (t *turn) sinkNow() *Sink {
	t.inter.mu.Lock()
	defer t.inter.mu.Unlock()
	return t.inter.sink
}

// finishSink reports the end of the turn to the sink holding its output, if
// that is not the Handle caller (which learns it from Handle's return).
func (t *turn) finishSink(out Outcome) {
	t.inter.mu.Lock()
	s, first := t.inter.sink, t.inter.first
	t.inter.mu.Unlock()
	if s == nil || s == first || s.Done == nil {
		return
	}
	select {
	case s.Done <- out:
	default:
	}
}

// eventMessage renders one inbox or web event as the user message a running
// turn takes in.
func (k *Kernel) eventMessage(ev link.Event) provider.Message {
	owners := k.Bot.Config().Owners
	raw := link.Render(ev, link.OwnerTrusted(ev.Source, ev.Sender, owners))
	content := raw
	if ev.ReplyTo > 0 {
		if q := k.quotePrefix(ev.ReplyTo); q != "" {
			content = q + "\n\n" + raw
		}
	}
	// An absorbed message is answered by this very turn, so its pictures
	// travel with it now. The names ride along for the transcript line: the
	// note naming them is added when the line is assembled, not here, so the
	// stored text stays the owner's words and is not noted twice.
	msg := provider.Message{
		Role:    provider.RoleUser,
		EventID: ev.ID,
		Content: content,
		ReplyTo: ev.ReplyTo,
	}
	if len(ev.Images) > 0 {
		msg.ImageNames = ev.Images
		// The note always goes in, not only when a picture is left out: the
		// agent may still trim to the turn's allowance, and a picture that
		// fell to the cap needs the path to be found again. The note is
		// removed again before the line is stored (recordTurn), so the
		// transcript keeps the owner's words and the assembled context adds
		// it back exactly once.
		msg.Content = k.withPastedNote(msg.Content, ev.Images)
		msg.Images = k.pastedImages(ev.Images)
	}
	return msg
}

// HandedOff reports whether a newer owner message took over the output of
// the turn this kernel last ran. The Handle caller then ends its stream
// without an answer of its own.
func (k *Kernel) HandedOff() bool {
	t := k.curTurn()
	t.inter.mu.Lock()
	defer t.inter.mu.Unlock()
	return t.inter.handed
}

// freshAllowanceLocked gives the foreground its full allowance again: an
// owner message taken in mid-turn is a new request. t.mu must be held.
func (k *Kernel) freshAllowanceLocked(t *turn) {
	t.capped = false
	t.capSteps = 0
	t.capBaseTools = t.tools
	t.capFrom = k.now()
}

// takeOwner returns the owner messages interjected into t and, when there
// are any, restarts the foreground allowance.
func (r *turnRun) takeOwner() []provider.Message {
	q := r.t.takeInterjected()
	if len(q) > 0 {
		r.t.mu.Lock()
		r.k.freshAllowanceLocked(r.t)
		r.t.mu.Unlock()
	}
	return q
}

// Failed generation does not remove accepted words. Associate the failure with
// each owner input using the ordinary transcript rather than an unread ledger.
func (k *Kernel) replyToFailedInputs(t *turn) error {
	t.mu.Lock()
	replyThrough := t.replyInputThrough
	t.mu.Unlock()
	n, err := k.Bot.Count()
	if err != nil {
		return err
	}
	rows, err := k.Bot.Slice(t.fromSeq, n)
	if err != nil {
		return err
	}
	for i, row := range rows {
		seq := t.fromSeq + i
		if seq <= replyThrough || row.Message.Role != provider.RoleUser || row.IsInternal() || row.IsForkMark() || strings.HasPrefix(row.Message.Content, "<untrusted_event") {
			continue
		}
		if _, err := k.recordTurn(t, provider.Message{Role: provider.RoleAssistant, ReplyTo: seq, Content: "这一轮出错，后续要求还没处理完；已经执行的记录和你的原话都保留了。"}); err != nil {
			return err
		}
	}
	return nil
}
