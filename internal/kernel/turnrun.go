package kernel

import (
	"context"
	"errors"
	"sort"
	"strings"

	"xbot/internal/agent"
	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/notable"
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
	err             error
	requestSeqs     []int
	referenceInputs []provider.Message
	// Request bounds for error reporting only. A late input may already be on
	// the transcript without being in the completed answer's request.
	sentThrough int
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

// seqAt maps the prepared request (including newly received words and environment
// differences) to the transcript for the existing context-length fitter.
func (r *turnRun) seqAt(i int) int {
	if i < 0 || i >= len(r.requestSeqs) {
		return 0
	}
	return r.requestSeqs[i]
}

// requestContext keeps the live conversation (including transient skill/tool
// bodies), and merges only newly persisted input/environment by transcript order.
func (r *turnRun) requestContext(req provider.Request) (provider.Request, error) {
	req.HostedSearch = req.Tools != nil && provider.HasHostedSearch(r.p)
	r.k.context.mu.Lock()
	r.k.context.tools = estimateTools(req.ToolDefinitions())
	r.k.context.mu.Unlock()
	if r.k.AllowFork {
		return req, nil
	}
	var out []provider.Message
	seen := map[int]bool{}
	through := 0
	for i, msg := range req.Messages {
		if i < len(r.assembled) {
			msg.Seq = r.assembled[i].Seq
		} else if j := i - len(r.assembled); j < len(r.msgSeqs) {
			msg.Seq = r.msgSeqs[j]
		}
		if msg.Seq > 0 {
			if seen[msg.Seq] && i >= len(r.assembled) {
				continue
			}
			seen[msg.Seq] = true
		}
		out = append(out, msg)
	}
	for _, msg := range r.assembled {
		if msg.Seq > through {
			through = msg.Seq
		}
	}
	err := r.k.Bot.Walk(through+1, 0, func(seq int, e bot.Entry) error {
		if seen[seq] || e.Message.Role != provider.RoleUser || e.Branch != "" || bot.IsApprovalCard(e.Message.Text()) || e.IsForkMark() {
			return nil
		}
		msg := e.Message
		msg.Seq, msg.Internal = seq, e.IsInternal()
		if e.ReplyTo > 0 {
			if q := r.k.quotePrefix(e.ReplyTo); q != "" {
				msg.SetText(q + "\n\n" + msg.Text())
			}
		}
		msg.SetText(r.k.withLineNote(msg.Role, msg.Text(), e.Images, e.Audios))
		out = append(out, msg)
		return nil
	})
	if err != nil {
		return req, err
	}
	// Only sort the chronological tail; stable prefix and current reminders keep
	// their original bytes. New inputs cannot move an earlier owner/assistant.
	pre := len(r.assembled)
	if pre > len(out) {
		pre = len(out)
	}
	tail := out[pre:]
	// Unknown-sequence summary instructions stay after the recorded messages.
	sort.SliceStable(tail, func(i, j int) bool {
		if tail[i].Seq == 0 {
			return false
		}
		if tail[j].Seq == 0 {
			return true
		}
		return tail[i].Seq < tail[j].Seq
	})
	out = strayToolResults(groupToolResults(out))
	r.requestSeqs = make([]int, len(out))
	for i := range out {
		r.requestSeqs[i] = out[i].Seq
	}
	req.Messages = out
	return req, nil
}

// options builds the agent callbacks for this run.
func (r *turnRun) options(approve agent.Approver) agent.Options {
	opts := agent.Options{
		ToolBatch:     r.t.toolBatch,
		RunTool:       r.k.runToolCall,
		MaxSteps:      r.k.stepGuard(),
		Approve:       approve,
		OnDelta:       r.delta,
		OnToolDisplay: r.toolStart,
		OnResult:      r.toolEnd,
		Redact:        r.k.redactor(),
		OnMessage:     r.onMessage,
		RefreshTools:  r.k.modelTools,
		AfterStep:     r.afterStep,
		Guard:         r.guardStep,
		OnIdle:        r.onIdle,
	}
	// A timer wake has no one to stream to, until an owner message takes
	// over its output.
	if r.onDelta == nil && r.k.AllowFork {
		opts.OnDelta = nil
	}
	if !r.k.AllowFork {
		opts.Preempt = r.t.preempt
		opts.Preempted = r.t.takeInterjected
	}
	return opts
}

func (r *turnRun) delta(s string) {
	if r.t.isForked() {
		return
	}
	if sk := r.t.sinkNow(); sk != nil && sk.OnDelta != nil {
		sk.OnDelta(s)
	}
}

func (r *turnRun) toolStart(id, name, summary, description string) {
	if name == "ask_user" {
		r.t.mu.Lock()
		r.t.askedOwner = true
		r.t.mu.Unlock()
	}
	display := r.k.beginStepDisplay(r.t, id, name, summary, description)
	// Delivery accounting needs the message text; other tools expose only their
	// description to the live progress display.
	if name == "send_message_to_user" {
		display = summary
	}
	r.t.mu.Lock()
	forked := r.t.branch != ""
	r.t.mu.Unlock()
	if sk := r.t.sinkNow(); sk != nil && sk.OnTool != nil && !forked {
		sk.OnTool(name, display)
	}
}

func (r *turnRun) toolEnd(id, name, output string) {
	r.k.endStep(r.t, id, output)
	if sk := r.t.sinkNow(); sk != nil && sk.OnResult != nil && !r.t.isForked() {
		sk.OnResult(name, output)
	}
}

func (r *turnRun) onMessage(m provider.Message) {
	if r.err != nil {
		return
	}
	if err := r.track(m); err != nil {
		r.err = err
		return
	}
	if text := finalAssistantText([]provider.Message{m}); text != "" && !bot.SilentText(text) {
		r.t.mu.Lock()
		r.t.replyInputThrough = max(r.t.replyInputThrough, r.sentThrough)
		r.t.mu.Unlock()
	}
}

// afterStep hands the model the messages that arrived during the step, or, for
// a turn that may fork, the controls addressed to its background.
func (r *turnRun) afterStep() ([]provider.Message, error) {
	if !r.k.AllowFork {
		// The owner's words go first, then whatever the inbox brought.
		owner := r.t.takeInterjected()
		rest, err := r.k.absorbInbox()
		messages := append(owner, rest...)
		r.referenceInputs = append(r.referenceInputs, messages...)
		return messages, err
	}
	r.t.forkMu.Lock()
	defer r.t.forkMu.Unlock()
	if r.t.isForked() || r.k.tryForkLocked(r.t) {
		// A background never reads the owner's messages; they belong to the
		// foreground. It reads only the controls sent to it: steer, queue,
		// interrupt.
		messages := r.t.takeCtrl()
		r.referenceInputs = append(r.referenceInputs, messages...)
		return messages, nil
	}
	messages, err := r.k.absorbInbox()
	r.referenceInputs = append(r.referenceInputs, messages...)
	return messages, err
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
	var referenceInput strings.Builder
	for _, m := range append(stepMsgs, r.referenceInputs...) {
		if !m.Internal && (m.Role == provider.RoleUser || m.Role == provider.RoleAssistant) {
			referenceInput.WriteString(m.Text())
			referenceInput.WriteByte('\n')
		}
	}
	r.referenceInputs = nil
	join.add(r.recordReminder(KindReference, r.k.referenceKeywordNote(referenceInput.String(), r.t.referenceProvided)))
	join.add(r.recordReminder(KindTodo, r.k.todoNote(r.k.now(), r.k.todoPrev())))
	// A shell job that finished during this step is attached here, after the
	// tool results, so it does not land in the middle of a call. Project
	// instructions and context usage use the same boundary: they show up on the
	// next model call, not in the middle of one.
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
		inner:        r.p,
		schemaKernel: r.k,
		prepare:      r.requestContext,
		finish:       r.finishEnvironment,
		accept:       r.k.userReplyTarget,
		// The snapshot is taken before the request leaves, so a call that times
		// out or fails still shows what was sent. It clips the text, so the ring
		// holds previews, not tool output.
		onSend: func(req provider.Request) {
			r.sentThrough = 0
			for _, msg := range req.Messages {
				r.sentThrough = max(r.sentThrough, msg.Seq)
			}
			r.k.noteContext(r.t, req)
		},
		onEnd: func(req provider.Request, msg provider.Message, err error) {
			r.k.noteCallUsage(r.t, req, msg, err)
			r.k.endContext(r.t, err)
		},
		validate: r.validateReply,
	}
	fit := newTurnFit(len(r.assembled), r.seqAt)
	fit.refreshEnvironment = !r.k.AllowFork
	rp.fit = func(ctx context.Context, req provider.Request, overflow bool) provider.Request {
		// Reserve the sampled replacement before fit, so a snapshot never bypasses H.
		st, err := r.k.Bot.LoadState()
		if err == nil && !r.k.AllowFork {
			prev := replayEnvironment(req.Messages, st.MemSnap)
			next, sampleErr := r.k.sampleEnvironment(prev)
			if sampleErr == nil {
				text := environmentDelta(prev, next, environmentBasis(st, req.Messages), st.MemSnap)
				texts, _, renderErr := r.k.environmentPrompts(ctx, text, true)
				if renderErr == nil {
					text = strings.Join(texts, "\n\n")
				}
				fit.reserve = r.k.scaled(compact.EstimateMessage(provider.Message{Role: provider.RoleUser, Content: provider.TextContent(text)}))
			}
		}
		return r.k.fit(ctx, r.t, fit, req, overflow)
	}
	produced, err := agent.Run(r.modelContext(), rp, r.list, r.assembled, opts)
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
		if len(pend) == 0 && !bg && !r.k.AllowFork && err == nil {
			// The owner spoke as the model finished: answer in this turn.
			// The lock order is t.mu, then t.inter.mu, the same everywhere.
			pend = r.k.closeInterject(r.t, false)
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
		var pendingText strings.Builder
		for _, m := range pend {
			if !m.Internal && m.Role == provider.RoleUser {
				pendingText.WriteString(m.Text())
				pendingText.WriteByte('\n')
			}
		}
		if note := r.recordReminder(KindReference, r.k.referenceKeywordNote(pendingText.String(), r.t.referenceProvided)); note != "" {
			convo = agent.AttachReminder(convo, note)
		}
		more, rerr := agent.Run(r.modelContext(), rp, r.list, convo, opts)
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
		r.k.flushToolJobs()
		r.t.cancelRun()
		return
	}
	r.k.flushToolJobs()
	if !r.k.AllowFork {
		// A failed run leaves accepted words on the transcript and reports the
		// error to the current sink. Handle appends their failure replies.
		dropped := r.k.closeInterject(r.t, true)
		r.t.mu.Lock()
		replyTo := r.t.replyTo
		r.t.mu.Unlock()
		if len(dropped) > 0 && err == nil {
			err = errors.New("插话尚未处理完成；原话已保留")
		}
		r.t.finishSink(Outcome{Err: err, ReplyTo: replyTo})
	}
	done <- turnResult{produced: produced, err: err}
}

func (r *turnRun) part(p provider.ContentPart) {
	if r.t.isForked() {
		return
	}
	m := r.k.scrubEntry(provider.Message{Role: provider.RoleAssistant, Content: []provider.ContentPart{p}})
	if sk := r.t.sinkNow(); sk != nil && sk.OnPart != nil {
		for _, p := range m.Content {
			p.VendorRaw = nil
			sk.OnPart(p)
		}
	}
}

func (r *turnRun) modelContext() context.Context {
	ctx := notable.With(r.t.runCtx, r.k.WorkDir, &r.t.notable)
	// Bind hosted delivery to this reply, including a continuation after an
	// earlier reply. A late completion cannot attach to a later generation.
	r.t.mu.Lock()
	gen := r.t.replyGen
	r.t.mu.Unlock()
	ctx = context.WithValue(ctx, callReplyGenKey{}, gen)
	if provider.HasHostedSearch(r.p) || provider.HasImageGen(ctx) {
		return provider.WithPartObserver(ctx, r.part)
	}
	return ctx
}
