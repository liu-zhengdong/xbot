// Package kernel wakes a bot for one event. It knows the inbox's events and the
// granted tools. It does not know which link produced the event.
package kernel

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"xbot/internal/agent"
	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/exec"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/notify"
	"xbot/internal/provider"
	"xbot/internal/tools"
	"xbot/internal/vault"
	"xbot/prompts"
)

// Kernel is the bot's turn runner.
type Kernel struct {
	Bot        *bot.Bot
	Provider   provider.Provider
	Links      []link.Link
	Inbox      *inbox.Inbox
	Memory     memory.Store
	Compactor  compact.Compactor
	Approve    agent.Approver
	MaxSteps   int
	Budget     compact.Budget
	SendPolicy link.SendPolicy
	OnDelta    func(string)
	OnTool     func(name, summary string)
	OnResult   func(name, output string)
	Selector   provider.ModelSelector
	Now        func() time.Time
	Extractor  Extractor
	// Redact overrides bot.RedactText for text this kernel returns, records, or
	// streams from a tool. Nil uses the process replacer.
	Redact func(string) string
	// Vault holds this bot's credentials. When set, the model gains an
	// outbound HTTP tool whose {{secret:NAME}} placeholders are filled at egress.
	Vault   vault.Backend
	Desktop func(ctx context.Context, title, body string) error
	// Exec runs subagents. Nil uses in-process goroutines, or a Runner link when one is mounted.
	Exec exec.Executor
	// Statuses, when set, supplies the bot status across every same-owner bot
	// including remote nodes. Nil falls back to this process's own bots.
	Statuses StatusSource
	// AllowBackground auto-approves confirm-needed tools inside a subagent.
	// The main chat approver is not used: a background step cannot block on a prompt.
	AllowBackground bool
	WorkDir         string
	SharedDir       string
	SkillsDir       string
	// Machine reports the local machine switch for tools that touch this
	// machine. Nil reads machines.json under the bot root; the zero value is
	// allow. off refuses, ask confirms each machine action, allow follows the
	// one policy table.
	Machine func() (string, error)

	// AllowFork moves a running turn to the background when the owner sends a
	// message mid-turn, so the next Handle can answer at once. See docs/atlas/fork.md.
	// ForkCap (default 3) caps concurrent backgrounds; ForkAfter (default 3s) is the
	// age a turn without any tool call must reach before it forks.
	AllowFork bool
	// BackgroundApprove asks the owner for a background turn's confirmation without
	// the request that started it, for example as a card in the transcript.
	BackgroundApprove agent.Approver
	ForkCap           int
	ForkAfter         time.Duration
	// ForegroundMaxTools (default 4) and ForegroundMaxTime (default 20s) cap a
	// main-line turn when AllowFork is on; past either, the turn moves to the
	// background if a slot is free. Negative turns a limit off.
	ForegroundMaxTools int
	ForegroundMaxTime  time.Duration
	// MaxSegments (default 4) caps how many segments one background task may
	// run, the first one included, when a segment ends at its step cap with the
	// task unfinished. BackgroundMaxTime (default 20m) caps the wall-clock time
	// all those segments may spend together; a negative value turns it off.
	MaxSegments       int
	BackgroundMaxTime time.Duration

	turnMu sync.Mutex
	// memOwnMu guards memOwn: line changes of this bot's own remember/forget
	// calls, from any turn, drained into State at the next wake (drainMemOwn).
	memOwnMu sync.Mutex
	memOwn   []ownChange
	// memWriteMu serializes own memory writes with their before/after reads.
	memWriteMu  sync.Mutex
	bgMu        sync.Mutex
	bgs         map[string]*turn
	finished    []BackgroundInfo
	startOnce   sync.Once
	recoverOnce sync.Once
	execOnce    sync.Once
	loadMu      sync.Mutex
	loaded      map[string]bool
	linksMu     sync.Mutex
	secretMu    sync.Mutex
	secrets     map[string]map[string]string
	compactMu   sync.Mutex
	compactBusy bool
	turn        *turn
	// channel is the reminder sidecar for this kernel. Lazy, so tests that
	// never remind pay only the first touch.
	channel *Channel
	// files is this bot's read state for the life of the kernel. Another bot's
	// kernel does not see it, and a new process starts empty.
	fileMu sync.Mutex
	files  map[string]*fileRec
	fileN  uint64
	// denied remembers tool calls the owner refused during the current turn.
	// Handle clears it when a new turn starts, so a later message can ask again.
	denyMu sync.Mutex
	denied map[string]struct{}

	// TodoSteps is how many tool steps a list must sit unchanged before a
	// reminder. Zero uses the model spec's todo_steps, or 6.
	TodoSteps int

	todoMu     sync.Mutex
	todoCached bool
	todoOnV    bool
	todoStepsV int
	todoLoaded bool
	todoMem    todoFile

	// shell jobs live for this kernel, which is the serve process. A new kernel
	// loads the same directory and marks anything still running as lost.
	shellOnce    sync.Once
	shell        *tools.Jobs
	shellMu      sync.Mutex
	shellClaimed map[string]bool
	// forms cache: redactable vault forms, invalidated when a vault generation
	// changes. Staged secrets are read on each call; they are few and short.
	formsOK   bool
	forms     []string
	formsVGen uint64
	formsRGen uint64

	// cwd is the persistent shell working directory for this bot.
	cwd cwdState
	// projects caches the AGENTS.md chain for the current directory stamp.
	projects projectCache
	// tokens is model-reported usage for this kernel's lifetime.
	tokens tokenState
}

// Handle runs one event to a final assistant message, or returns the agent error.
// A repeated event whose turn is already complete does not call the model again.
func (k *Kernel) Handle(ctx context.Context, ev link.Event) error {
	if err := ev.Validate(); err != nil {
		return err
	}
	// Once per kernel lifetime, before repairTail: a branch left open by a crash
	// gets its fork_end so the dangling call on it can be repaired on that branch.
	k.recoverOnce.Do(k.RecoverInterrupted)
	t := &turn{
		turnSender:  ev.Sender,
		turnKind:    ev.SenderKind,
		turnHops:    ev.Hops,
		turnChain:   ev.Chain,
		turnSource:  ev.Source,
		turnEvent:   ev.ID,
		turnTrusted: link.OwnerTrusted(ev.Source, ev.Sender, k.Bot.Config().Owners),
	}
	t.started = k.now()
	t.task = clipRunes(strings.TrimSpace(ev.Content), forkTaskRunes)
	t.forked = make(chan struct{})
	k.turnMu.Lock()
	k.turn = t
	k.turnMu.Unlock()
	k.clearDenials()
	k.clearLoaded()
	if _, err := k.releaseQuiet(ctx); err != nil {
		return err
	}
	// Swap a finished background summary on the way in and on the way out.
	// Both run outside the model call, including when this event is a replay.
	defer func() {
		cur, loadErr := k.Bot.LoadState()
		if loadErr != nil {
			return
		}
		_, _, _ = k.applyReadyJob(migrate(cur))
	}()
	if err := k.repairTail(); err != nil {
		return err
	}
	n, err := k.Bot.Count()
	if err != nil {
		return err
	}
	seq, err := k.Bot.FindEvent(ev.ID)
	if err != nil {
		return err
	}
	if seq > 0 {
		span, err := k.Bot.Slice(seq, n)
		if err != nil {
			return err
		}
		if turnDone(span, 0) {
			t.replyTo = lastReplyTo(span, 0)
			if k.OnDelta != nil && ev.Source != link.NameTimer {
				if text := lastAssistant(span, 0); text != "" {
					k.OnDelta(text)
				}
			}
			return nil
		}
	}
	st, err := k.Bot.LoadState()
	if err != nil {
		return err
	}
	st = migrate(st)
	if key := k.boundModelKey(); key != "" {
		st.BoundModel = key
	}
	// memWriteMu keeps a background turn's own write from landing between this
	// read and the drain, which would report it as removed.
	key := k.memViewKey(t, st)
	k.memWriteMu.Lock()
	memText, err := k.memoryText(ctx, key)
	if err != nil {
		k.memWriteMu.Unlock()
		return err
	}
	if st.MemRender != memRenderVersion {
		st.MemSnapSet = false
		st.MemSnap, st.MemSeen = "", ""
		st.MemOwn, st.MemOther = nil, nil
		st.MemRender = memRenderVersion
	}
	switchMemView(&st, key, memText, k.Bot.Config().Owners)
	k.drainMemOwn(&st)
	k.memWriteMu.Unlock()
	st, err = k.syncEnv(st, ev, memText)
	if err != nil {
		return err
	}
	if seq == 0 && t.turnTrusted {
		if note := backgroundNote(k.Backgrounds()); note != "" {
			if err := k.Bot.Append(bot.Entry{Time: k.now(), Message: provider.Message{Role: provider.RoleUser, Content: note}}); err != nil {
				return err
			}
		}
	}
	if seq == 0 {
		entry := bot.Entry{
			Time:    ev.Time,
			EventID: ev.ID,
			ReplyTo: ev.ReplyTo,
			Message: provider.Message{Role: provider.RoleUser, Content: link.Render(ev, t.turnTrusted)},
		}
		if err := k.Bot.Append(entry); err != nil {
			return err
		}
		seq, err = k.Bot.Count()
		if err != nil {
			return err
		}
	}
	st, _, err = k.applyReadyJob(st)
	if err != nil {
		return err
	}
	st, err = k.autoCompact(ctx, st, seq, st.MemSnap)
	if err != nil {
		return err
	}
	k.maybeStartBackground(st, seq, st.MemSnap)
	if err := k.ensureSnapshot(st); err != nil {
		return err
	}
	assembled, usage, err := k.assemble(st, st.MemSnap)
	if err != nil {
		return err
	}
	// Reminders stick to the newest message. The sidecar replays them on the
	// next wake so that message's bytes stay what the model already saw.
	// Timer wakes skip the pending-card kind; the trigger returns empty.
	seqNow, seqErr := k.newestReminderSeq(st.Blocks)
	if seqErr != nil {
		return seqErr
	}
	before := 0
	if n := len(assembled); n > 0 {
		before = compact.EstimateMessage(assembled[n-1])
	}
	assembled, err = k.reminderKinds(ev).Bind(assembled, seqNow, k.now())
	if err != nil {
		return err
	}
	if n := len(assembled); n > 0 {
		if delta := compact.EstimateMessage(assembled[n-1]) - before; delta > 0 {
			usage.Env += delta
		}
	}
	st.Usage = usage
	if err := k.Bot.SaveState(st); err != nil {
		return err
	}
	p, err := k.providerFor(ctx)
	if err != nil {
		return err
	}
	list, err := k.modelTools()
	if err != nil {
		return err
	}
	onDelta := k.OnDelta
	if ev.Source == link.NameTimer {
		onDelta = nil
	}
	var appendErr error
	var approve agent.Approver
	if k.Approve != nil {
		approve = k.trackApprove(k.Approve)
	}
	if approve != nil {
		inner := approve
		approve = func(ctx context.Context, tool, summary string) (bool, error) {
			k.beginAwait(t, tool)
			defer k.endAwait(t)
			return inner(ctx, tool, summary)
		}
	}
	if k.AllowFork && (approve != nil || k.BackgroundApprove != nil) {
		inner := approve
		approve = func(ctx context.Context, tool, summary string) (bool, error) {
			if t.isForked() {
				// The request that could show a card is gone; a background's card goes
				// to the owner through BackgroundApprove, or the call is declined.
				if k.BackgroundApprove == nil {
					return false, nil
				}
				return k.trackApprove(k.BackgroundApprove)(ctx, tool, summary)
			}
			if inner == nil {
				return false, nil
			}
			return inner(ctx, tool, summary)
		}
	}
	t.fromSeq = seq
	// A turn that may fork runs on a context the caller's cancel reaches only until
	// it forks; a background outlives the request that started it.
	runCtx, cancelRun := context.WithCancel(ctx)
	stopWatch := func() bool { return false }
	if k.AllowFork {
		runCtx, cancelRun = context.WithCancel(context.WithoutCancel(ctx))
		stopWatch = context.AfterFunc(ctx, cancelRun)
	}
	runCtx = context.WithValue(runCtx, turnKey{}, t)
	t.runCtx = runCtx
	t.cancelRun = cancelRun
	br := newBreaker()
	var msgSeqs []int
	track := func(m provider.Message) error {
		seq, err := k.recordTurn(t, m)
		if err != nil {
			return err
		}
		msgSeqs = append(msgSeqs, seq)
		return nil
	}
	opts := agent.Options{
		MaxSteps: k.MaxSteps,
		Approve:  approve,
		OnDelta: func(s string) {
			if onDelta != nil && !t.isForked() {
				onDelta(s)
			}
		},
		OnTool: func(name, summary string) {
			if name == "ask_user" {
				t.mu.Lock()
				t.askedOwner = true
				t.mu.Unlock()
			}
			k.beginStep(t, name, summary)
			t.mu.Lock()
			forked := t.branch != ""
			t.mu.Unlock()
			if k.OnTool != nil && !forked {
				k.OnTool(name, summary)
			}
		},
		OnResult: func(name, output string) {
			k.endStep(t, name, output)
			if k.OnResult != nil && !t.isForked() {
				k.OnResult(name, output)
			}
		},
		Redact: k.redactor(),
		// Hitting the step cap is not an error: it just means this segment is out
		// of room, which lets a background start the next one.
		OnCap: func() {
			t.mu.Lock()
			t.capStopped = true
			t.mu.Unlock()
		},
		OnStep: func() {
			t.mu.Lock()
			t.stepsUsed++
			t.mu.Unlock()
		},
		OnMessage: func(m provider.Message) {
			if appendErr != nil {
				return
			}
			appendErr = track(m)
		},
		RefreshTools: func() ([]tools.Tool, error) {
			return k.modelTools()
		},
		AfterStep: func() ([]provider.Message, error) {
			if !k.AllowFork {
				return k.absorbInbox()
			}
			t.forkMu.Lock()
			defer t.forkMu.Unlock()
			if t.isForked() || k.tryForkLocked(t) {
				// A background never reads the owner's messages; they belong to the foreground.
				// It reads only the controls sent to it: steer, queue, interrupt.
				return t.takeCtrl(), nil
			}
			return k.absorbInbox()
		},
		Guard: func(_ int, stepMsgs []provider.Message) (string, bool, string) {
			k.noteTodoStep(stepMsgs)
			remind, stop, reason := br.Observe(stepMsgs)
			if remind != "" {
				// The reminder was attached to the message just recorded, not to
				// whatever Count returns after a concurrent foreground write.
				if rerr := k.reminders().Record(t.lastSeq(), KindLoop, remind, k.now()); rerr != nil && appendErr == nil {
					appendErr = rerr
				}
			}
			if todo := k.todoNote(k.now(), k.todoPrev()); todo != "" {
				if rerr := k.reminders().Record(t.lastSeq(), KindTodo, todo, k.now()); rerr != nil && appendErr == nil {
					appendErr = rerr
				}
				if remind != "" {
					remind += "\n\n" + todo
				} else {
					remind = todo
				}
			}
			// A shell job that finished during this step is attached here, after
			// the tool results, so it does not land in the middle of a call.
			// Project instructions and context usage use the same boundary:
			// they show up on the next model call, not in the middle of one.
			if note := k.shellNotice(k.shellPrev()); note != "" {
				if rerr := k.reminders().Record(t.lastSeq(), KindShellJob, note, k.now()); rerr != nil && appendErr == nil {
					appendErr = rerr
				}
				if remind != "" {
					remind += "\n\n" + note
				} else {
					remind = note
				}
			}
			if note := k.stepBoundaryNotes(t); note != "" {
				if remind != "" {
					remind += "\n\n" + note
				} else {
					remind = note
				}
			}
			if stop {
				_ = k.Bot.LogTrace(bot.TraceStop, reason, k.now())
			}
			return remind, stop, reason
		},
		OnIdle: func(msg provider.Message) (string, bool) {
			if br.Stopped() {
				return "", false
			}
			// The gate is a new user message, recorded by OnMessage. Binding it
			// onto the assistant line would end the request on an assistant turn
			// and make replay attribute the text to the model.
			return k.todoGate(t, msg)
		},
	}
	if onDelta == nil {
		opts.OnDelta = nil
	}
	type result struct {
		produced []provider.Message
		err      error
	}
	done := make(chan result, 1)
	go func() {
		rp := &replyProvider{inner: p, accept: k.userReplyTarget, onUse: func(req provider.Request, msg provider.Message) {
			k.noteUsage(t, req, msg)
		}}
		recordLoop := func(text string) {
			if text == "" {
				return
			}
			if rerr := k.reminders().Record(t.lastSeq(), KindLoop, text, k.now()); rerr != nil && appendErr == nil {
				appendErr = rerr
			}
		}
		// haltBreaker is the hard stop between segments. Inside a segment the
		// Guard path does the same thing via agent.stopForReason.
		haltBreaker := func(why string, produced *[]provider.Message) {
			if lerr := k.Bot.LogTrace(bot.TraceStop, why, k.now()); lerr != nil && appendErr == nil {
				appendErr = lerr
			}
			note := provider.Message{Role: provider.RoleUser, Content: agent.StopNote(why)}
			if rerr := track(note); rerr != nil && appendErr == nil {
				appendErr = rerr
			}
			convo := append(append([]provider.Message(nil), assembled...), *produced...)
			convo = append(convo, note)
			*produced = append(*produced, note)
			msg, cerr := rp.Chat(runCtx, provider.Request{Messages: convo}, opts.OnDelta)
			if cerr != nil || strings.TrimSpace(msg.Content) == "" {
				msg = provider.Message{Role: provider.RoleAssistant, Content: "Stopped (" + why + ") before the task finished."}
			}
			msg.Role = provider.RoleAssistant
			msg.ToolCalls = nil
			if rerr := track(msg); rerr != nil && appendErr == nil {
				appendErr = rerr
			}
			*produced = append(*produced, msg)
		}
		produced, err := agent.Run(runCtx, rp, list, assembled, opts)
		var bg bool
		for {
			if err == nil {
				err = appendErr
			}
			// A queued control that arrived after the last step starts another run, so a
			// background works through its queue before it ends. A background whose
			// segment ended at the step cap auto-continues for the same reason, until
			// its segment, step, or wall-clock budget runs out. The budget is decided
			// before any control is consumed, so a refused segment leaves the controls
			// pending and finishBackground reports them as unread.
			t.mu.Lock()
			bg = t.branch != ""
			var reason string
			var more bool
			if err == nil && bg {
				more, reason = k.continueSegmentLocked(t, len(t.pendingCtrlLocked()) > 0)
			}
			// Controls stay pending until the next segment is actually allowed to
			// start. A breaker stop must not mark them applied.
			atCap := t.capStopped
			finishing := err != nil || !bg || !more
			if finishing {
				t.finished = true
			} else {
				t.capStopped = false
			}
			t.mu.Unlock()
			if finishing {
				if bg && err == nil {
					remind, stop, why := br.EndContinuation()
					recordLoop(remind)
					if stop {
						haltBreaker(why, &produced)
						reason = breakerHaltReason(why)
					}
				}
				k.markSegmentStop(t, reason)
				break
			}
			// A cap continuation is the unit diminishing returns counts. The
			// opening segment is not one. A second trip refuses the next segment
			// and gives back the segment count continueSegmentLocked just took.
			if more && atCap {
				remind, stop, why := br.BeginContinuation()
				if stop {
					t.mu.Lock()
					if t.segments > 0 {
						t.segments--
					}
					t.finished = true
					t.mu.Unlock()
					haltBreaker(why, &produced)
					k.markSegmentStop(t, breakerHaltReason(why))
					break
				}
				recordLoop(remind)
			} else {
				br.ResetSegment()
			}
			t.mu.Lock()
			pend := t.takeCtrlLocked()
			t.mu.Unlock()
			// Another run starts fresh: its own end decides whether the task is done.
			// Reminders already recorded stay on the message they were bound to.
			// They are not copied onto whatever line is newest in this segment.
			convo := append(append([]provider.Message(nil), assembled...), produced...)
			if err := k.reminders().Apply(convo[len(assembled):], msgSeqs); err != nil && appendErr == nil {
				appendErr = err
			}
			if more && atCap {
				n := provider.Message{Role: provider.RoleUser, Content: continueNote}
				if rerr := track(n); rerr != nil && appendErr == nil {
					appendErr = rerr
				}
				convo = append(convo, n)
				produced = append(produced, n)
			}
			for _, m := range pend {
				if rerr := track(m); rerr != nil && appendErr == nil {
					appendErr = rerr
				}
				convo = append(convo, m)
				produced = append(produced, m)
			}
			more2, rerr := agent.Run(runCtx, rp, list, convo, opts)
			produced = append(produced, more2...)
			err = rerr
		}
		// The turn is over. Every request inside it still carried the tool
		// images. Drop the bytes now so this slice is not a later turn.
		// The transcript never stored them.
		for i := range produced {
			produced[i].Images = nil
		}
		if errors.Is(err, context.Canceled) {
			if serr := k.closeDangling(DangleCancel, false); serr != nil && appendErr == nil {
				appendErr = serr
			}
		}
		if bg {
			k.finishBackground(t, produced, err)
			k.flushShellWake()
			cancelRun()
			return
		}
		k.flushShellWake()
		done <- result{produced, err}
	}()
	var tick <-chan time.Time
	if k.AllowFork {
		ticker := time.NewTicker(forkPoll)
		defer ticker.Stop()
		tick = ticker.C
	}
	for {
		select {
		case r := <-done:
			stopWatch()
			cancelRun()
			if appendErr != nil {
				return appendErr
			}
			if pushErr := k.pushFindings(ctx, r.produced); pushErr != nil {
				return pushErr
			}
			return r.err
		case <-t.forked:
			// The turn goes on in the background; the caller may answer the owner now.
			stopWatch()
			t.mu.Lock()
			capped, id := t.capFork, t.branch
			t.mu.Unlock()
			if capped {
				note := capNotice(id)
				if err := k.Bot.Append(bot.Entry{Time: k.now(), Message: provider.Message{Role: provider.RoleAssistant, Content: note}}); err != nil {
					return err
				}
				if onDelta != nil {
					onDelta(note)
				}
			}
			return nil
		case <-tick:
			k.tryFork(t)
		}
	}
}

// absorbInbox appends inbox events that arrived during this turn, oldest first.
// The event being handled stays pending until the caller acks it.
func (k *Kernel) absorbInbox() ([]provider.Message, error) {
	if k.Inbox == nil {
		return nil, nil
	}
	if err := k.Inbox.Reload(); err != nil {
		return nil, err
	}
	var out []provider.Message
	owners := k.Bot.Config().Owners
	for _, ev := range k.Inbox.Pending() {
		if ev.ID == "" || ev.ID == k.curTurn().turnEvent {
			continue
		}
		raw := link.Render(ev, link.OwnerTrusted(ev.Source, ev.Sender, owners))
		content := raw
		if ev.ReplyTo > 0 {
			if q := k.quotePrefix(ev.ReplyTo); q != "" {
				content = q + "\n\n" + raw
			}
		}
		out = append(out, provider.Message{
			Role:    provider.RoleUser,
			Content: content,
			ReplyTo: ev.ReplyTo,
		})
		if err := k.Inbox.Ack(ev.ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (k *Kernel) providerFor(ctx context.Context) (provider.Provider, error) {
	if k.Selector != nil {
		return k.Selector.Select(ctx, provider.Hint{Bot: k.Bot.Name()})
	}
	if k.Provider == nil {
		return nil, errors.New("kernel: no provider")
	}
	return k.Provider, nil
}

func (k *Kernel) assemble(st bot.State, mem string) ([]provider.Message, bot.SectionUsage, error) {
	persona, err := k.Bot.ComposedPersona()
	if err != nil {
		return nil, bot.SectionUsage{}, err
	}
	rules, err := k.Bot.ReadRules(DefaultRules())
	if err != nil {
		return nil, bot.SectionUsage{}, err
	}
	skills, err := k.skillLines()
	if err != nil {
		return nil, bot.SectionUsage{}, err
	}
	_, overlay, err := k.modelOverlay()
	if err != nil {
		return nil, bot.SectionUsage{}, err
	}
	installed, err := k.installedSection()
	if err != nil {
		return nil, bot.SectionUsage{}, err
	}
	lines, err := k.Bot.UncoveredLines(st.Blocks)
	if err != nil {
		return nil, bot.SectionUsage{}, err
	}
	minSeq := 0
	if len(lines) > 0 {
		minSeq = lines[0].Seq
	}
	if err := k.reminders().Compact(minSeq); err != nil {
		return nil, bot.SectionUsage{}, err
	}
	raw := make([]bot.Entry, 0, len(lines))
	seqs := make([]int, 0, len(lines))
	for _, ln := range lines {
		e := ln.Entry
		if e.ReplyTo > 0 && e.Message.Role == provider.RoleUser {
			if q := k.quotePrefix(e.ReplyTo); q != "" {
				e.Message.Content = q + "\n\n" + e.Message.Content
			}
		}
		raw = append(raw, e)
		seqs = append(seqs, ln.Seq)
	}
	raw, err = k.reminders().Replay(raw, seqs)
	if err != nil {
		return nil, bot.SectionUsage{}, err
	}
	msgs, usage := account(Input{
		Persona:   persona,
		Rules:     rules,
		Links:     k.stableLinkNames(),
		Memory:    mem,
		Blocks:    st.Blocks,
		Skills:    skills,
		Installed: installed,
		Overlay:   overlay,
	}, projected(st.Blocks, raw))
	return msgs, usage, nil
}

func (k *Kernel) syncEnv(st bot.State, ev link.Event, mem string) (bot.State, error) {
	persona, err := k.Bot.ReadPersona()
	if err != nil {
		return st, err
	}
	rules, err := k.Bot.ReadRules(DefaultRules())
	if err != nil {
		return st, err
	}
	list, err := k.modelTools()
	if err != nil {
		return st, err
	}
	skills, err := k.skillLines()
	if err != nil {
		return st, err
	}
	skillHash := ""
	if len(skills) > 0 {
		skillHash = shortHash(strings.Join(skills, "\n"))
	}
	profile := k.Bot.ProfileText()
	profileHash := ""
	if profile != "" {
		profileHash = shortHash(profile)
	}
	overlayKey, overlay, err := k.modelOverlay()
	if err != nil {
		return st, err
	}
	overlayHash := ""
	if overlay != "" {
		overlayHash = shortHash(overlay)
	}
	// The memory snapshot in msgs[1] stays frozen; what changed since the bot last
	// saw memory is appended here as a diff, minus the bot's own changes (MemOwn).
	memBlock := ""
	// Own changes count even when live equals MemSeen: someone may have undone them.
	if st.MemSnapSet && (mem != st.MemSeen || len(st.MemOwn) > 0) {
		memBlock = memoryDiff(st.MemSeen, mem, st.MemOwn)
	}
	st.MemSeen, st.MemOwn = mem, nil
	now := k.now()
	next := bot.EnvRecord{
		Hour:        now.Format("2006-01-02T15"),
		Now:         k.localStamp(),
		Sender:      ev.Sender,
		Tools:       toolNames(list),
		Notes:       k.pendingNotes(),
		Persona:     shortHash(persona),
		Rules:       shortHash(rules),
		Memory:      shortHash(st.MemSeen),
		Skills:      skillHash,
		Profile:     profileHash,
		OverlayKey:  overlayKey,
		OverlayHash: overlayHash,
		OverlayBody: overlay,
		At:          now,
	}
	diff := DiffEnv(st.Env, next)
	st.Env = next
	if memBlock != "" {
		if diff == "" {
			diff = "<environment_update>\n" + memBlock + "\n</environment_update>"
		} else {
			diff = strings.TrimSuffix(diff, "</environment_update>") + memBlock + "\n</environment_update>"
		}
	}
	if diff == "" {
		return st, nil
	}
	entry := bot.Entry{Time: now, Message: provider.Message{Role: provider.RoleUser, Content: diff}}
	if err := k.Bot.Append(entry); err != nil {
		return st, err
	}
	return st, nil
}

func (k *Kernel) modelOverlay() (string, string, error) {
	if k == nil || k.Bot == nil {
		return "", "", nil
	}
	cfg := k.Bot.Config()
	if strings.TrimSpace(cfg.Provider) == "" || strings.TrimSpace(cfg.Model) == "" {
		return "", "", nil
	}
	text, err := prompts.Load(k.Bot.Root, cfg.Provider, cfg.Model)
	if err != nil || text == "" {
		return "", text, err
	}
	return cfg.Provider + "/" + cfg.Model, text, nil
}

func (k *Kernel) ensureSnapshot(st bot.State) error {
	if st.Env.At.IsZero() {
		return nil
	}
	raw, err := k.Bot.Uncovered(st.Blocks)
	if err != nil {
		return err
	}
	if uncoveredHasEnv(raw) {
		return nil
	}
	entry := bot.Entry{Time: k.now(), Message: provider.Message{Role: provider.RoleUser, Content: snapshotText(st.Env)}}
	return k.Bot.Append(entry)
}

func (k *Kernel) repairTail() error {
	// A call on a background still running here is in flight, not lost.
	return k.closeDangling(k.danglingReason(), true)
}

func (k *Kernel) newestReminderSeq(blocks []bot.SummaryBlock) (int, error) {
	if k == nil || k.Bot == nil {
		return 0, nil
	}
	lines, err := k.Bot.UncoveredLines(blocks)
	if err != nil {
		return 0, err
	}
	return newestSeq(lines), nil
}

func (k *Kernel) pendingNotes() int {
	notes, err := notify.Open(k.Bot.OutboxPath()).Unread()
	if err != nil {
		return 0
	}
	return len(notes)
}

// memoryPerson is the sender only when they are a person.
// A bot, a group, or a subagent must not be looked up as someone else's memory.
// A subagent maps to the parent bot's owner, or to no person scope when the bot has none.
func (k *Kernel) memoryPerson() string {
	return k.memoryPersonOf(k.curTurn())
}

// memoryPersonOf is memoryPerson for a given turn, so a background turn's tools
// render memory for their own sender rather than the foreground turn's.
func (k *Kernel) memoryPersonOf(t *turn) string {
	if t == nil {
		return ""
	}
	if t.turnKind == link.KindBot || t.turnKind == link.KindGroup {
		return ""
	}
	if t.turnSource == link.SourceSubagent || strings.HasPrefix(t.turnSender, "sa_") {
		return k.ownerPerson()
	}
	return t.turnSender
}

func (k *Kernel) ownerPerson() string {
	if k == nil || k.Bot == nil {
		return ""
	}
	for _, id := range k.Bot.Config().Owners {
		if id != "" {
			return id
		}
	}
	return ""
}

// memViewKey picks whose memory view a turn uses: the person's own view, or the
// shared bot-only view ("") for groups, peer bots, and connectors. Timer, shell,
// and subagent wakes (including a background's fork_end) are the bot's own; they
// keep the active view, so the prefix does not flip and a guest's background
// result is not read next to the owner's person facts.
func (k *Kernel) memViewKey(t *turn, st bot.State) string {
	if st.MemSnapSet && t != nil && (t.turnSource == link.NameTimer || t.turnSource == "shell" || t.turnSource == link.SourceSubagent) {
		return st.MemPerson
	}
	if person := k.memoryPersonOf(t); memory.ValidID(person) {
		return person
	}
	return ""
}

// memViewCap bounds the parked views kept in State.
const memViewCap = 8

// switchMemView makes the view for key active. Each view keeps its own frozen
// snapshot, so another person's turn never gets the owner's person facts in
// msgs[1], and the owner's view comes back unchanged. A new key gets a fresh
// snapshot of live memory. Past memViewCap parked views, one non-owner view is
// dropped; an owner's view is never dropped.
func switchMemView(st *bot.State, key, live string, owners []string) {
	if st.MemSnapSet && st.MemPerson == key {
		return
	}
	// Take the target out first, so eviction cannot drop the view being switched to.
	v, ok := st.MemOther[key]
	if ok {
		delete(st.MemOther, key)
	} else {
		v = bot.MemView{Snap: live, Seen: live}
	}
	if st.MemSnapSet {
		if st.MemOther == nil {
			st.MemOther = map[string]bot.MemView{}
		}
		if len(st.MemOther) >= memViewCap {
			evictMemView(st.MemOther, owners)
		}
		st.MemOther[st.MemPerson] = bot.MemView{Snap: st.MemSnap, Seen: st.MemSeen, Own: st.MemOwn}
	}
	st.MemSnap, st.MemSeen, st.MemOwn, st.MemPerson, st.MemSnapSet = v.Snap, v.Seen, v.Own, key, true
}

func evictMemView(views map[string]bot.MemView, owners []string) {
	keys := make([]string, 0, len(views))
	for key := range views {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !slices.Contains(owners, key) {
			delete(views, key)
			return
		}
	}
}

// ownChange is one own write's line changes. person is the view it belongs to
// for a person-scope write, or "" for a bot-scope write, which every view shares.
type ownChange struct {
	person string
	lines  []string
}

// beginOwnWrite starts recording one own remember/forget. It holds memWriteMu
// until the returned func runs after the write, so parallel tool calls cannot
// mix their before/after texts. A bot-scope write is measured on the bot-only
// rendering, so it can never carry person lines into another person's view.
// A failed read records nothing; the write then shows as a diff, which is safe.
func (k *Kernel) beginOwnWrite(ctx context.Context, scope memory.Scope, personID ...string) func() {
	k.memWriteMu.Lock()
	key := ""
	if scope == memory.ScopePerson {
		st, _ := k.Bot.LoadState()
		key = k.memViewKey(k.turnFor(ctx), st)
		if len(personID) > 0 {
			key = personID[0]
		}
	}
	before, err := k.memoryText(ctx, key)
	return func() {
		defer k.memWriteMu.Unlock()
		if err != nil {
			return
		}
		after, err := k.memoryText(ctx, key)
		if err != nil {
			return
		}
		c := ownChange{person: key, lines: ownLines(before, after)}
		if len(c.lines) == 0 {
			return
		}
		k.memOwnMu.Lock()
		k.memOwn = append(k.memOwn, c)
		if len(k.memOwn) > memOwnCap {
			k.memOwn = k.memOwn[len(k.memOwn)-memOwnCap:]
		}
		k.memOwnMu.Unlock()
	}
}

// memOwnCap bounds the own writes waiting for the next wake.
const memOwnCap = 200

// drainMemOwn moves pending own changes into every view they apply to: bot-scope
// changes go to all views, person-scope changes only to that person's view.
func (k *Kernel) drainMemOwn(st *bot.State) {
	k.memOwnMu.Lock()
	pending := k.memOwn
	k.memOwn = nil
	k.memOwnMu.Unlock()
	for _, c := range pending {
		if c.person == "" || c.person == st.MemPerson {
			st.MemOwn = append(st.MemOwn, c.lines...)
		}
		for key, v := range st.MemOther {
			if c.person == "" || c.person == key {
				v.Own = append(v.Own, c.lines...)
				st.MemOther[key] = v
			}
		}
	}
}

const memRenderVersion = 2

// refreshMemSnap re-snapshots standing memory after a compaction appended a block.
// The prefix already changed, so the active view is replaced and parked views are
// dropped. The active view keeps its key, whichever turn runs the compaction.
// A read error keeps the old snapshot; the next wake diffs as usual. The new
// snapshot already holds the bot's pending own writes, so the queue is cleared
// under memWriteMu, which also waits out a write in progress.
func (k *Kernel) refreshMemSnap(ctx context.Context, st *bot.State) {
	key := st.MemPerson
	if !st.MemSnapSet {
		key = k.memViewKey(k.curTurn(), *st)
	}
	k.memWriteMu.Lock()
	defer k.memWriteMu.Unlock()
	mem, err := k.memoryText(ctx, key)
	if err != nil {
		return
	}
	k.memOwnMu.Lock()
	k.memOwn = nil
	k.memOwnMu.Unlock()
	st.MemRender = memRenderVersion
	st.MemSnap, st.MemSeen, st.MemSnapSet, st.MemPerson = mem, mem, true, key
	st.MemOwn, st.MemOther = nil, nil
}

// memSnapFor returns the frozen snapshot of the view key, or live memory when
// this session has no view for it yet.
func (k *Kernel) memSnapFor(ctx context.Context, st bot.State, key string) (string, error) {
	if st.MemSnapSet && st.MemPerson == key {
		return st.MemSnap, nil
	}
	if v, ok := st.MemOther[key]; ok {
		return v.Snap, nil
	}
	return k.memoryText(ctx, key)
}

func (k *Kernel) memoryText(ctx context.Context, personID string) (string, error) {
	facts, logs, err := k.loadMemory(ctx, personID)
	if err != nil {
		return "", err
	}
	paths := map[memory.Scope]string{}
	if k.Bot != nil {
		shown := func(path string) string {
			for _, l := range k.snapshotLinks() {
				if l != nil {
					for _, tool := range l.Tools() {
						if tool.Name() == "write_file" {
							return modelPath(tool, path)
						}
					}
				}
			}
			return path
		}
		dir := filepath.Join(k.Bot.Root, "bots", k.Bot.Name(), "memory")
		paths[memory.ScopeBot] = shown(filepath.Join(dir, "memory.md")) + "; " + shown(filepath.Join(dir, "log")) + "/"
		if personID != "" {
			paths[memory.ScopePerson] = shown(filepath.Join(k.Bot.Root, "people", personID, "memory", "memory.md"))
		}
	}
	if counter, ok := k.Memory.(interface {
		LogCount(memory.Scope, string, string) (int, error)
	}); ok {
		total, err := counter.LogCount(memory.ScopeBot, k.Bot.Name(), "")
		if err != nil {
			return "", err
		}
		return clipMemory(renderMemoryPaths(facts, logs, paths, total)), nil
	}
	return clipMemory(renderMemoryPaths(facts, logs, paths)), nil
}

// loadMemory is the context-assembly seam for the person and bot scopes.
// M1's store recalls nothing. M2 returns facts; Visible and Resolve still apply.
// logs are bot-scope log facts: the snapshot shows the newest, recall has the rest.
func (k *Kernel) loadMemory(ctx context.Context, personID string) ([]memory.Fact, []memory.Fact, error) {
	if k.Memory == nil {
		return nil, nil, nil
	}
	botName := k.Bot.Name()
	var personFacts []memory.Fact
	// External senders such as webhook:github are not person ids. Bot memory still loads.
	if memory.ValidID(personID) {
		var err error
		personFacts, err = k.Memory.Recall(ctx, memory.Query{Scope: memory.ScopePerson, PersonID: personID, Tier: memory.TierProfile})
		if err != nil {
			return nil, nil, err
		}
	}
	botFacts, err := k.Memory.Recall(ctx, memory.Query{Scope: memory.ScopeBot, BotName: botName, Tier: memory.TierProfile})
	if err != nil {
		return nil, nil, err
	}
	logFacts, err := k.Memory.Recall(ctx, memory.Query{Scope: memory.ScopeBot, BotName: botName, Tier: memory.TierLog})
	if err != nil {
		return nil, nil, err
	}
	all := append(personFacts, botFacts...)
	return memory.Resolve(memory.Visible(all, botName, personID)), memory.Visible(logFacts, botName, personID), nil
}

// StartEmitters runs granted links that implement link.Emitter.
// Calling it twice does not start a second copy. M1 grants no emitter.
func (k *Kernel) StartEmitters(ctx context.Context) {
	k.startOnce.Do(func() {
		if k.Inbox == nil {
			return
		}
		for _, l := range k.snapshotLinks() {
			em, ok := l.(link.Emitter)
			if !ok {
				continue
			}
			go func(em link.Emitter) {
				_ = em.Run(ctx, func(ev link.Event) error {
					ev.Untrusted = true
					if ev.Time.IsZero() {
						ev.Time = time.Now().UTC()
					}
					return k.Inbox.Post(ev)
				})
			}(em)
		}
	})
}

// Close releases links that hold a process, such as an MCP server a short-lived
// kernel connected while calling a tool. It does not stop emitters on a kernel
// that is still in use.
func (k *Kernel) Close() error {
	if k == nil {
		return nil
	}
	k.shellMu.Lock()
	jobs := k.shell
	k.shellMu.Unlock()
	if jobs != nil {
		jobs.Shutdown()
	}
	var first error
	for _, l := range k.snapshotLinks() {
		c, ok := l.(interface{ Close() error })
		if !ok || c == nil {
			continue
		}
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Deliver is the outbound hook for messages on the user's behalf.
// The default policy drafts first and sends only after Approve returns true.
func (k *Kernel) Deliver(ctx context.Context, m link.Messenger, to, body string) error {
	preview, err := m.Draft(ctx, to, body)
	if err != nil {
		return err
	}
	approved := false
	if !k.SendPolicy.AutoSend {
		if k.Approve != nil {
			approved, err = k.trackApprove(k.Approve)(ctx, m.Name(), preview)
			if err != nil {
				return err
			}
		}
	}
	if !k.SendPolicy.Allowed(approved) {
		return link.ErrNotSent
	}
	return m.SendDraft(ctx, to, body)
}

// BudgetFor derives the compaction threshold from the bot config.
// A context limit compacts at three quarters of that limit.
func BudgetFor(cfg bot.Config) compact.Budget {
	b := compact.DefaultBudget()
	if cfg.ContextLimit > 0 {
		b.CompactAt = cfg.ContextLimit * 3 / 4
		if b.CompactAt < 1 {
			b.CompactAt = 1
		}
	}
	return b
}
