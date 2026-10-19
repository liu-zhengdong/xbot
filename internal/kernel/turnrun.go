package kernel

import (
	"context"
	"errors"
	"strings"

	"xbot/internal/agent"
	"xbot/internal/bot"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

// turnRun is the state one Handle call owns while the model works: the turn,
// the transcript lines it recorded, the breaker, and the first append error.
//
// The callbacks handed to agent.Run used to be closures over locals in Handle,
// which made the run's real state (which error wins, which sequence numbers
// were recorded) visible only by reading all of Handle. They now write here, so
// each stage below can be read on its own.
type turnRun struct {
	k    *Kernel
	t    *turn
	p    provider.Provider
	list []tools.Tool
	// assembled is the context the first model call receives. A later run in the
	// same turn appends to a copy of it, never to this slice.
	assembled []provider.Message
	onDelta   func(string)
	br        *Breaker

	// msgSeqs are the transcript lines this turn recorded, in order, so the
	// reminder sidecar can bind text to the message it belongs to.
	msgSeqs []int
	// err is the first transcript error. The model loop keeps going so its
	// output is not dropped; the error is returned once the run ends.
	err error
}

// turnResult is what the model loop hands back to Handle.
type turnResult struct {
	produced []provider.Message
	err      error
}

// fail records err unless an earlier one already won.
func (r *turnRun) fail(err error) {
	if err != nil && r.err == nil {
		r.err = err
	}
}

// track appends one message to the transcript and remembers its sequence.
func (r *turnRun) track(m provider.Message) error {
	seq, err := r.k.recordTurn(r.t, m)
	if err != nil {
		return err
	}
	r.msgSeqs = append(r.msgSeqs, seq)
	return nil
}

// options builds the agent callbacks for this run.
func (r *turnRun) options(approve agent.Approver) agent.Options {
	opts := agent.Options{
		MaxSteps:     r.k.stepGuard(),
		Approve:      approve,
		OnDelta:      r.delta,
		OnTool:       r.toolStart,
		OnResult:     r.toolEnd,
		Redact:       r.k.redactor(),
		OnMessage:    r.onMessage,
		RefreshTools: func() ([]tools.Tool, error) { return r.k.modelTools() },
		AfterStep:    r.afterStep,
		Guard:        r.guardStep,
		OnIdle:       r.onIdle,
	}
	// A timer wake has no one to stream to.
	if r.onDelta == nil {
		opts.OnDelta = nil
	}
	return opts
}

func (r *turnRun) delta(s string) {
	if r.onDelta != nil && !r.t.isForked() {
		r.onDelta(s)
	}
}

func (r *turnRun) toolStart(name, summary string) {
	if name == "ask_user" {
		r.t.mu.Lock()
		r.t.askedOwner = true
		r.t.mu.Unlock()
	}
	r.k.beginStep(r.t, name, summary)
	r.t.mu.Lock()
	forked := r.t.branch != ""
	r.t.mu.Unlock()
	if r.k.OnTool != nil && !forked {
		r.k.OnTool(name, summary)
	}
}

func (r *turnRun) toolEnd(name, output string) {
	r.k.endStep(r.t, name, output)
	if r.k.OnResult != nil && !r.t.isForked() {
		r.k.OnResult(name, output)
	}
}

func (r *turnRun) onMessage(m provider.Message) {
	if r.err != nil {
		return
	}
	if err := r.track(m); err != nil {
		r.err = err
	}
}

// afterStep hands the model the messages that arrived during the step, or, for
// a turn that may fork, the controls addressed to its background.
func (r *turnRun) afterStep() ([]provider.Message, error) {
	if !r.k.AllowFork {
		return r.k.absorbInbox()
	}
	r.t.forkMu.Lock()
	defer r.t.forkMu.Unlock()
	if r.t.isForked() || r.k.tryForkLocked(r.t) {
		// A background never reads the owner's messages; they belong to the
		// foreground. It reads only the controls sent to it: steer, queue,
		// interrupt.
		return r.t.takeCtrl(), nil
	}
	return r.k.absorbInbox()
}

func (r *turnRun) onIdle(msg provider.Message) (string, bool) {
	if r.br.Stopped() {
		return "", false
	}
	// The gate is a new user message, recorded by onMessage. Binding it onto the
	// assistant line would end the request on an assistant turn and make replay
	// attribute the text to the model.
	return r.k.todoGate(r.t, msg)
}

// recordReminder stores one reminder on the line this turn just recorded and
// returns its text. A store error is reported once, when the run ends.
func (r *turnRun) recordReminder(kind, text string) string {
	if text == "" {
		return ""
	}
	if err := r.k.reminders().Record(r.t.lastSeq(), kind, text, r.k.now()); err != nil {
		r.fail(err)
	}
	return text
}

// guardStep runs at the boundary between two model steps. It returns the
// reminder to attach to the line just recorded, and whether the run must stop.
//
// Each reminder source is one call below. A new source is one more line here,
// not another branch on the accumulated string.
func (r *turnRun) guardStep(_ int, stepMsgs []provider.Message) (string, bool, string) {
	r.k.noteTodoStep(stepMsgs)
	remind, stop, reason := r.br.Observe(stepMsgs)

	// recordReminder returns "" for an empty text, so one call both stores the
	// reminder and reports what to append.
	var join textJoin
	join.add(r.recordReminder(KindLoop, remind))
	join.add(r.recordReminder(KindTodo, r.k.todoNote(r.k.now(), r.k.todoPrev())))
	// A shell job that finished during this step is attached here, after the
	// tool results, so it does not land in the middle of a call. Project
	// instructions and context usage use the same boundary: they show up on the
	// next model call, not in the middle of one.
	join.add(r.recordReminder(KindShellJob, r.k.shellNotice(r.k.shellPrev())))
	join.add(r.k.stepBoundaryNotes(r.t))

	if !stop {
		if why, over := r.k.backgroundOverBudget(r.t); over {
			stop, reason = true, why
		}
	}
	if stop {
		_ = r.k.Bot.LogTrace(bot.TraceStop, reason, r.k.now())
	}
	return join.String(), stop, reason
}

// textJoin joins non-empty blocks with a blank line, the separator the model
// expects between distinct reminders.
type textJoin struct {
	b strings.Builder
	n int
}

func (j *textJoin) add(s string) {
	if s == "" {
		return
	}
	if j.n > 0 {
		j.b.WriteString("\n\n")
	}
	j.b.WriteString(s)
	j.n++
}

func (j *textJoin) String() string { return j.b.String() }

// drive runs the model loop for this turn and reports to done, or finishes the
// background itself. It owns every write to r.err and r.msgSeqs during the run.
func (r *turnRun) drive(opts agent.Options, done chan<- turnResult) {
	rp := &replyProvider{
		inner:  r.p,
		accept: r.k.userReplyTarget,
		// The snapshot is taken before the request leaves, so a call that times
		// out or fails still shows what was sent. It clips the text, so the ring
		// holds previews, not tool output.
		onSend: func(req provider.Request) { r.k.noteContext(r.t, req) },
		onUse:  func(req provider.Request, msg provider.Message) { r.k.noteUsage(r.t, req, msg) },
	}
	fit := newTurnFit(len(r.assembled))
	rp.fit = func(ctx context.Context, req provider.Request, overflow bool) provider.Request {
		return r.k.fit(ctx, r.t, fit, req, overflow)
	}
	produced, err := agent.Run(r.t.runCtx, rp, r.list, r.assembled, opts)
	var bg bool
	for {
		if err == nil {
			err = r.err
		}
		// A control that arrived after the last step starts another run, so a
		// background works through its queue before it ends. Nothing else
		// continues it: a background stops when the model stops, when the
		// breaker trips, or when its wall-clock budget is spent. Controls are
		// read only once another run is certain: a run the budget refuses leaves
		// them pending, and finishBackground reports them as unread.
		r.t.mu.Lock()
		bg = r.t.branch != ""
		var pend []provider.Message
		if err == nil && bg && len(r.t.pendingCtrlLocked()) > 0 {
			if spent, over := r.k.backgroundBudgetSpent(r.t); over {
				r.t.stopReason = spent
			} else {
				pend = r.t.takeCtrlLocked()
			}
		}
		finishing := len(pend) == 0
		if finishing {
			r.t.finished = true
		}
		r.t.mu.Unlock()
		if finishing {
			break
		}
		// Another run starts fresh: its own end decides whether the task is
		// done. Reminders already recorded stay on the message they were bound
		// to; they are not copied onto whatever line is newest.
		r.br.ResetSegment()
		convo := append(append([]provider.Message(nil), r.assembled...), produced...)
		r.fail(r.k.reminders().Apply(convo[len(r.assembled):], r.msgSeqs))
		for _, m := range pend {
			r.fail(r.track(m))
			convo = append(convo, m)
			produced = append(produced, m)
		}
		more, rerr := agent.Run(r.t.runCtx, rp, r.list, convo, opts)
		produced = append(produced, more...)
		err = rerr
	}
	// The turn is over. Every request inside it still carried the tool images.
	// Drop the bytes now so this slice is not a later turn. The transcript never
	// stored them.
	for i := range produced {
		produced[i].Images = nil
	}
	if errors.Is(err, context.Canceled) {
		r.fail(r.k.closeDangling(DangleCancel, false))
	}
	if bg {
		r.k.finishBackground(r.t, produced, err)
		r.k.flushShellWake()
		r.t.cancelRun()
		return
	}
	r.k.flushShellWake()
	done <- turnResult{produced: produced, err: err}
}
