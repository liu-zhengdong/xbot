// Package kernel wakes a bot for one event. It knows the inbox's events and the
// granted tools. It does not know which link produced the event.
//
// # Lock order
//
// A Kernel lives for the whole serve process and is shared by the foreground
// turn, background branches, the exec forwarder and MCP emitters. Most of its
// locks guard one domain and are never held together. Only two functions hold
// two locks at once, and both take them in the same direction:
//
//	bgMu -> turn.mu          Queue, cancelQueued (bgturn.go)
//
// No function anywhere in this package takes turn.mu and then bgMu. Keep it
// that way: a background finishing while the owner sends a message is the
// normal case, so the reverse order would deadlock a live bot rather than a
// test.
//
// Every other lock is taken and released inside one function before the next
// is taken; they are listed here in the order the outer work runs, so a new
// caller can see where it belongs:
//
//	envMu       environment publication (may take memWriteMu, then memOwnMu)
//	turnMu      the foreground turn pointer (Kernel.turn)
//	bgMu        the background map (Kernel.bgs, finished)
//	turn.mu     one turn's own state (branch, steps, tools, todo flags)
//	forkMu      the fork decision against absorbing the inbox
//	memWriteMu  a memory write against its own before/after read
//	memOwnMu    this bot's own memory line changes
//	todo.mu     the task list
//	files.mu    read state for the freshness check
//	shellMu     the background shell job set
//	secretMu    staged secrets for one turn
//	linksMu     mounted links and their grants
//	loadMu      which links are loaded
//	compactMu   the auto-compaction job
//	denyMu      tool calls the owner refused this turn
//
// The *Once fields (startOnce, recoverOnce, execOnce, shellOnce) never nest:
// each body runs once and may take other locks freely, because nothing holds a
// lock across the call that starts them.
package kernel

import (
	"context"
	"errors"
	"path/filepath"
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
	Bot       *bot.Bot
	Provider  provider.Provider
	Links     []link.Link
	Inbox     *inbox.Inbox
	Memory    memory.Store
	Compactor compact.Compactor
	Approve   agent.Approver
	// ApprovalWait configures ask_user's deadline; zero uses bot.DefaultCardWait.
	ApprovalWait time.Duration
	// MaxSteps is a runaway guard, not a design limit: a turn ends when the
	// model stops, when the breaker trips, or when the background budget is
	// spent. Zero uses maxStepsGuard. Tests set it small to keep fixtures short.
	MaxSteps   int
	Budget     compact.Budget
	SendPolicy link.SendPolicy
	OnDelta    func(string)
	OnTool     func(name, summary string)
	OnResult   func(name, output string)
	// Permissive turns the permission table's cards off (ADR 0019,
	// review.ConfirmTable): only an explicit machine setting, the vault, a
	// turn from outside touching memory, and the links card still ask.
	Permissive bool
	// OnHandoff runs when a newer owner message takes over the running
	// turn's output (Interject); the caller can end its own stream.
	OnHandoff func()
	Selector  provider.ModelSelector
	Now       func() time.Time
	Extractor Extractor
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

	// Dispatch makes the foreground the one that understands, plans, and hands
	// out work (ADR 0016): it gets dispatch instead of spawn, keeps only quick
	// read tools and memory edits, and the work runs in typed subagents.
	Dispatch bool

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
	// BackgroundMaxTime caps how long one background task may keep working. It
	// comes from the bot's role; a negative value turns it off. The background
	// turn's own context carries the deadline (see backgroundBudget).
	BackgroundMaxTime time.Duration

	envMu  sync.Mutex // serializes environment comparison and publication
	turnMu sync.Mutex
	// memOwnMu guards memOwn: line changes of this bot's own memory writes
	// from any turn, drained into State at the next wake (drainMemOwn).
	memOwnMu sync.Mutex
	memOwn   []ownChange
	// memWriteMu serializes own memory writes with their before/after reads.
	memWriteMu  sync.Mutex
	bgMu        sync.Mutex
	bgs         map[string]*turn
	finished    []BackgroundInfo
	startOnce   sync.Once
	recoverOnce sync.Once
	recoverErr  error
	execOnce    sync.Once
	execMu      sync.RWMutex
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
	files freshState
	// denied remembers tool calls the owner refused during the current turn.
	// Handle clears it when a new turn starts, so a later message can ask again.
	denyMu sync.Mutex
	denied map[string]struct{}

	// TodoSteps is how many tool steps a list must sit unchanged before a
	// reminder. Zero uses the model spec's todo_steps, or 6.
	TodoSteps int

	// todo is this bot's task list and its reminder policy.
	todo todoState

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
	// context keeps the recent requests of this wake for the web inspector.
	// Memory only; see noteContext.
	context contextRing
}

// maxStepsGuard bounds one run so a model that never stops cannot spin
// forever. It is far past any real task: a background is bounded by its
// wall-clock budget and by the breaker, not by counting steps.
const maxStepsGuard = 1000

// stepGuard is the step limit in effect for one run.
func (k *Kernel) stepGuard() int {
	if k.MaxSteps > 0 {
		return k.MaxSteps
	}
	return maxStepsGuard
}

// Handle runs one event to a final assistant message, or returns the agent error.
// A repeated event whose turn is already complete does not call the model again.
func (k *Kernel) Handle(ctx context.Context, ev link.Event) (handleErr error) {

	if err := ev.Validate(); err != nil {
		return err
	}

	existing, err := k.Bot.FindEvent(ev.ID)
	if err != nil {
		return err
	}
	seq, err := k.receive(ev)
	if err != nil {
		return err
	}
	// Once per kernel lifetime, before repairTail: a branch left open by a crash
	// gets its fork_end so the dangling call on it can be repaired on that branch.
	if err := k.RecoverTasks(); err != nil {
		return err
	}
	t := &turn{
		turnSender:  ev.Sender,
		turnKind:    ev.SenderKind,
		turnHops:    ev.Hops,
		turnChain:   ev.Chain,
		turnSource:  ev.Source,
		turnEvent:   ev.ID,
		turnTrusted: link.OwnerTrusted(ev.Source, ev.Sender, k.Bot.Config().Owners),
	}
	t.fromSeq = seq
	defer func() {
		if handleErr != nil && !errors.Is(handleErr, context.Canceled) && !k.AllowFork {
			handleErr = errors.Join(handleErr, k.replyToFailedInputs(t))
		}
	}()
	t.started = k.now()
	t.task = clipRunes(strings.TrimSpace(ev.Content), forkTaskRunes)
	t.forked = make(chan struct{})
	k.turnMu.Lock()
	k.turn = t
	k.turnMu.Unlock()
	// The owner can speak into this turn from the moment it exists, so the
	// time spent on setup (compaction, memory) is not a window where a message
	// misses it. A message that arrives before the first model call stops
	// that call before it leaves. Setup failures leave the original words and
	// a normal failure reply, even when their old stream has handed off.
	first := &Sink{OnDelta: k.OnDelta, OnTool: k.OnTool, OnResult: k.OnResult, OnHandoff: k.OnHandoff}
	if ev.Source == link.NameTimer {
		first.OnDelta = nil
	}
	k.openInterject(t, first)
	defer func() {
		if unread := k.closeInterject(t, true); len(unread) > 0 {
			t.finishSink(Outcome{Err: errors.New("这一轮出错，插话原文已保留")})
		}
	}()
	// A forked turn still belongs to its background goroutine. All other
	// returns, including setup/repair errors, end this foreground turn.
	defer func() {
		t.mu.Lock()
		if t.branch == "" {
			t.finished = true
		}
		t.mu.Unlock()
	}()
	k.clearDenials()
	k.clearLoaded()
	// A new wake sends a new context; the panel shows this wake, not the last.
	k.resetContext()
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
	if existing == 0 && ev.Source != link.NameApproval && link.OwnerTrusted(ev.Source, ev.Sender, k.Bot.Config().Owners) {
		AnswerQuestions(k.Bot, ev.Content)
	}
	if existing > 0 {
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
	key := ""
	k.memWriteMu.Lock()
	memText, err := k.memoryText(ctx, key)
	if err != nil {
		k.memWriteMu.Unlock()
		return err
	}
	if st.MemRender != memRenderVersion {
		st.MemSnapSet = false
		st.MemSnap, st.MemSeen = "", ""
		st.MemOwn = nil
		st.MemRender = memRenderVersion
	}
	if !st.MemSnapSet {
		st.MemSnap, st.MemSeen, st.MemSnapSet = memText, memText, true
	}
	k.memWriteMu.Unlock()
	st, err = k.syncEnv(st, ev, memText)
	if err != nil {
		return err
	}
	// The opening message is on the transcript, so this turn now owns this
	// ctx. The synchronous compaction below may summarize and extract, and
	// those calls belong to the turn that started them.
	ctx = withTurnSeq(ctx, seq)
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
	seqNow := seq
	before := 0
	if n := len(assembled); n > 0 {
		before = compact.EstimateMessage(assembled[n-1])
	}
	// The project trigger asks whether a set is already in this context:
	// replayed reminders are in assembled, and Bind adds any new one.
	for _, m := range assembled {
		t.projects.noteText(m.Content)
	}
	carrier := len(assembled) - 1
	for i := range assembled {
		if assembled[i].Seq == seqNow && !assembled[i].Internal {
			carrier = i
			break
		}
	}
	bound, bindErr := k.reminderKindsFor(ev, t).Bind(assembled[:carrier+1], seqNow, k.now())
	err = bindErr
	assembled = append(bound, assembled[carrier+1:]...)
	if err != nil {
		return err
	}
	// The turn answering a message with pictures sees them; a later turn only
	// sees the note assemble added. This runs after Bind, so the bytes sit on
	// the same message the reminders landed on and nothing is inserted between
	// a tool call and its result.
	if seq > 0 && len(ev.Images) > 0 {
		var added int
		assembled, added = k.attachEventImages(assembled, ev.Images)
		usage.History += added
	}
	if n := len(assembled); n > 0 {
		t.projects.noteText(assembled[n-1].Content)
	}
	if n := len(assembled); n > 0 {
		if delta := compact.EstimateMessage(assembled[n-1]) - before; delta > 0 {
			usage.Env += delta
		}
	}
	st.Usage = usage
	if err := k.saveState(st); err != nil {
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
	var approve agent.Approver
	if k.Approve != nil {
		approve = k.trackApprove(k.Approve)
	}
	if approve != nil {
		inner := approve
		approve = func(ctx context.Context, tool, summary string) (bool, error) {
			k.beginAwait(t, agent.ToolCallID(ctx))
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
	if !k.AllowFork {
		// Even with nobody to ask, an owner's grant from a parked card lets
		// that one call run.
		approve = k.parkable(t, approve)
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
	r := &turnRun{
		k:         k,
		t:         t,
		p:         p,
		list:      list,
		assembled: assembled,
		onDelta:   onDelta,
		br:        newBreaker(),
	}
	opts := r.options(approve)
	done := make(chan turnResult, 1)
	go r.drive(opts, done)
	var tick <-chan time.Time
	if k.AllowFork {
		ticker := time.NewTicker(forkPoll)
		defer ticker.Stop()
		tick = ticker.C
	}
	for {
		select {
		case res := <-done:
			stopWatch()
			cancelRun()
			if r.err != nil {
				return r.err
			}
			if pushErr := k.pushFindings(ctx, res.produced); pushErr != nil {
				return pushErr
			}
			return res.err
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
	for _, ev := range k.Inbox.Pending() {
		if ev.ID == "" || ev.ID == k.curTurn().turnEvent {
			continue
		}
		old, err := k.Bot.FindEvent(ev.ID)
		if err != nil {
			return nil, err
		}
		if old > 0 && old < k.curTurn().fromSeq {
			n, err := k.Bot.Count()
			if err != nil {
				return nil, err
			}
			span, err := k.Bot.Slice(old, n)
			if err != nil {
				return nil, err
			}
			if turnDone(span, 0) {
				if err := k.Inbox.Ack(ev.ID); err != nil {
					return nil, err
				}
				continue
			}
		}
		msg := k.eventMessage(ev)
		seq, err := k.receive(ev)
		if err != nil {
			return nil, err
		}
		msg.Seq = seq
		out = append(out, msg)
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

// safetyRules are part of every bot's rules. rules.md is written once, when the
// bot is created, so a bot created before one of these existed, or whose owner
// edited its rules, gets the missing ones appended here.
var safetyRules = []string{UntrustedRule, OwnerRule, ProjectRule, MemoryRule}

// rulesText is the bot's rules, any missing safety rule, and, when a memory
// store is attached, MemoryWriteRule. They are appended here rather than kept
// in rules.md so bots with a saved rules file get them too, and they sit in the
// system prompt, not the memory data block.
func (k *Kernel) rulesText() (string, error) {
	rules, err := k.Bot.ReadRules(DefaultRules())
	if err != nil {
		return rules, err
	}
	rules = strings.ReplaceAll(rules, legacyVerifyRule, VerifyRule)
	if !strings.Contains(rules, VoiceRule) {
		rules += "\n\n" + VoiceRule
	}
	for _, r := range safetyRules {
		if !strings.Contains(rules, r) {
			rules += "\n\n" + r
		}
	}
	if k.Permissive {
		rules = strings.Replace(rules, ActionRule, OwnAskActionRule, 1)
		if !strings.Contains(rules, EventMemoryRule) {
			rules += "\n\n" + EventMemoryRule
		}
	}
	if k.dispatching() && !strings.Contains(rules, DispatchRule) {
		rules += "\n\n" + DispatchRule
	}
	if k.Memory == nil || strings.Contains(rules, MemoryWriteRule) {
		return rules, nil
	}
	return rules + "\n\n" + MemoryWriteRule, nil
}

func (k *Kernel) assemble(st bot.State, mem string) ([]provider.Message, bot.SectionUsage, error) {
	persona, err := k.Bot.ComposedPersona()
	if err != nil {
		return nil, bot.SectionUsage{}, err
	}
	rules, err := k.rulesText()
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
		// A pasted picture is named here, on the line that carried it. Only the
		// turn answering that line gets the bytes; see attachEventImages.
		e.Message.Content = k.withPastedNote(e.Message.Content, e.Images)
		raw = append(raw, e)
		seqs = append(seqs, ln.Seq)
	}
	raw, seqs, err = k.reminders().Replay(raw, seqs)
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
	}, projected(st.Blocks, raw, seqs))
	return msgs, usage, nil
}

func (k *Kernel) syncEnv(st bot.State, ev link.Event, mem string) (bot.State, error) {
	k.envMu.Lock()
	defer k.envMu.Unlock()
	fresh, err := k.Bot.LoadState()
	if err != nil {
		return st, err
	}
	st.Env = fresh.Env
	if st.MemRender == fresh.MemRender {
		st.MemOwn = append([]string(nil), fresh.MemOwn...)
	}
	k.drainMemOwn(&st)
	persona, err := k.Bot.ReadPersona()
	if err != nil {
		return st, err
	}
	rules, err := k.rulesText()
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
		Hour:      now.Format("2006-01-02T15"),
		Now:       k.localStamp(),
		Sender:    st.Env.Sender,
		SenderSeq: st.Env.SenderSeq,
		Source:    st.Env.Source, PersonActive: st.Env.PersonActive,
		People:      st.Env.People,
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
	next.Tasks, err = k.taskEnvironment(st.Env.Tasks, ev)
	if err != nil {
		return st, err
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
		return st, k.Bot.SaveState(st)
	}
	entry := bot.Entry{Time: now, Origin: bot.OriginInternal, Message: provider.Message{Role: provider.RoleUser, Content: diff}}
	if err := k.Bot.Append(entry); err != nil {
		return st, err
	}
	return st, k.Bot.SaveState(st)
}

func (k *Kernel) modelOverlay() (string, string, error) {
	if k == nil || k.Bot == nil {
		return "", "", nil
	}
	cfg := k.Bot.Config()
	if strings.TrimSpace(cfg.Provider) == "" {
		// Unbound app bots use a concrete client from the environment. Match
		// its actual model, rather than assuming the product default model.
		if client, ok := k.Provider.(*provider.Client); ok && strings.Contains(strings.ToLower(client.Model), "deepseek") {
			cfg.Provider, cfg.Model = "deepseek", client.Model
		}
	}
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
	k.envMu.Lock()
	defer k.envMu.Unlock()
	fresh, err := k.Bot.LoadState()
	if err != nil {
		return err
	}
	if fresh.Env.At.IsZero() {
		return nil
	}
	raw, err := k.Bot.Uncovered(st.Blocks)
	if err != nil {
		return err
	}
	// A compacted person baseline must be made visible even if an unrelated
	// environment update survived the compressed range.
	person := fresh.Env.People[fresh.Env.Sender]
	needsPerson := fresh.Env.PersonActive && !person.Seen
	if uncoveredHasEnv(raw) && !needsPerson {
		return nil
	}
	entry := bot.Entry{Time: k.now(), Origin: bot.OriginInternal, Message: provider.Message{Role: provider.RoleUser, Content: snapshotText(fresh.Env)}}
	if err := k.Bot.Append(entry); err != nil {
		return err
	}
	if needsPerson {
		person.Seen = true
		fresh.Env.People[fresh.Env.Sender] = person
		return k.Bot.SaveState(fresh)
	}
	return nil
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

// ownChange is one own write's line changes. person is the view it belongs to
// for a person-scope write, or "" for a bot-scope write, which every view shares.
type ownChange struct {
	person string
	lines  []string
}

// beginOwnWrite starts recording one own memory write. It holds memWriteMu
// until the returned func runs after the write, so parallel tool calls cannot
// mix their before/after texts. A bot-scope write is measured on the bot-only
// rendering, so it can never carry person lines into another person's view.
// A failed read records nothing; the write then shows as a diff, which is safe.
func (k *Kernel) beginOwnWrite(ctx context.Context, scope memory.Scope, personID ...string) func(...bool) {
	k.memWriteMu.Lock()
	key := ""
	if scope == memory.ScopePerson {
		key = k.memoryPersonOf(k.turnFor(ctx))
		if len(personID) > 0 {
			key = personID[0]
		}
	}
	before, err := k.memoryText(ctx, key)
	return func(record ...bool) {
		defer k.memWriteMu.Unlock()
		if err != nil || (len(record) > 0 && !record[0]) {
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
		if c.person == "" {
			st.MemOwn = append(st.MemOwn, c.lines...)
		} else if person, ok := st.Env.People[c.person]; ok {
			person.Own = append(person.Own, c.lines...)
			st.Env.People[c.person] = person
		}
	}
}

const memRenderVersion = 3

// refreshMemSnap only refreshes the bot's snapshot when compaction changes the prefix.
func (k *Kernel) refreshMemSnap(ctx context.Context, st *bot.State) error {
	// Compaction can remove any visible person block. Keep the data and
	// invalidate the seen versions; each person re-establishes a full baseline
	// on their next appearance, using the same EnvRecord and publication lock.
	k.envMu.Lock()
	defer k.envMu.Unlock()
	k.memWriteMu.Lock()
	defer k.memWriteMu.Unlock()
	mem, err := k.memoryText(ctx, "")
	if err != nil {
		return err
	}
	fresh, err := k.Bot.LoadState()
	if err != nil {
		return err
	}
	for id, person := range fresh.Env.People {
		person.Seen, person.Own = false, nil
		fresh.Env.People[id] = person
	}
	if err := k.Bot.SaveState(fresh); err != nil {
		return err
	}
	st.Env = fresh.Env
	k.memOwnMu.Lock()
	k.memOwn = nil
	k.memOwnMu.Unlock()
	st.MemRender = memRenderVersion
	st.MemSnap, st.MemSeen, st.MemSnapSet = mem, mem, true
	st.MemOwn = nil
	return nil
}

// memSnapFor always returns this bot's one frozen snapshot, irrespective of sender.
func (k *Kernel) memSnapFor(ctx context.Context, st bot.State, _ string) (string, error) {
	if st.MemSnapSet {
		return st.MemSnap, nil
	}
	return k.memoryText(ctx, "")
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

// BudgetFor is the budget a bot starts with. Both lines are left at zero, so
// the kernel derives them from the bound model's window and the bot's
// context_limit on each use (see Kernel.lines), and a model change moves them.
func BudgetFor(bot.Config) compact.Budget {
	return compact.DefaultBudget()
}
