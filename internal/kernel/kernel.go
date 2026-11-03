// Package kernel wakes a bot for one event. It knows the inbox's events and the
// granted tools. It does not know which link produced the event.
//
// # Lock order
//
// Kernels may be replaced between serve passes. BotResources owns shared Jobs
// and MCP sessions. Turn state, real subagents and operation leases keep their
// original owners. Most locks guard one domain and are never held together.
//
// Environment sampling may also take approvals.mu or promptEnv.mu under envMu;
// neither projection takes envMu. Every other lock is taken and released before
// the next is taken; they are listed here in the order the outer work runs, so a new
// caller can see where it belongs:
//
//	operations.mu local profile/old-tool rename; then Bot profile snapshot and
//	              config file locks; released before calling a live Sink
//	envMu       environment publication and input append (may sample approvals)
//	approvals.mu live card projection; never acquires envMu
//	turnMu      the foreground turn pointer (Kernel.turn)
//	turn.mu     one turn's own state (branch, steps, tools, todo flags)
//	memWriteMu  a memory write against its own before/after read
//	todo.mu     the task list
//	files.mu    read state for the freshness check
//	shellMu     the tool job set
//	secretMu    staged secrets for one turn
//	linksMu     mounted links and their grants
//	loadMu      loaded links and expanded tool groups (separate namespaces)
//	compactMu   the auto-compaction job
//	denyMu      tool calls the owner refused this turn
//
// The *Once fields (startOnce, recoverOnce, legacyForkOnce, execOnce, shellOnce, promptOnce) never nest:
// each body runs once and may take other locks freely, because nothing holds a
// lock across the call that starts them, except promptOnce: registration may
// run under envMu and only takes promptEnv.mu.
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
	"xbot/internal/envprompt"
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
	Resources *BotResources
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
	OnPart     func(provider.ContentPart)
	OnTool     func(name, summary string)
	OnResult   func(name, output string)
	OnFinding  func(string)
	// OnProfile reports a saved profile through the current turn's output.
	OnProfile func(ProfileNotice)
	// Permissive permanently disables tool permission cards (ADR 0019).
	// Production sets this true; only links connector key input remains.
	Permissive bool
	// PermissionsOn is a legacy live switch for strict test/eval kernels.
	// It cannot override Permissive.
	PermissionsOn func() bool
	// OnHandoff runs when a newer owner message takes over the running
	// turn's output (Interject); the caller can end its own stream.
	OnHandoff func()
	Selector  provider.ModelSelector
	Now       func() time.Time
	// Redact overrides bot.RedactText for text this kernel returns, records, or
	// streams from a tool. Nil uses the process replacer.
	Redact func(string) string
	// Vault holds this bot's credentials. When set, the model gains an
	// outbound HTTP tool whose {{secret:NAME}} placeholders are filled at egress.
	Vault   vault.Backend
	Desktop func(ctx context.Context, title, body string) error
	// Restart requests a graceful process restart after persisting its continuation.
	// Nil means this kernel is not running in a restart-capable service.
	Restart func(hint string) error
	// Exec runs subagents. Nil uses in-process goroutines, or a Runner link when one is mounted.
	Exec exec.Executor
	// Statuses, when set, supplies the bot status across every same-owner bot
	// including remote nodes. Nil falls back to this process's own bots.
	Statuses StatusSource
	// AllowBackground auto-approves confirm-needed tools inside a subagent.
	// The main chat approver is not used: a background step cannot block on a prompt.
	AllowBackground bool
	WorkDir         string
	// workOrigin is the immutable directory component associated with WorkDir.
	// UseBot may refresh configuration between turns without changing this path fact.
	workOrigin string
	SharedDir  string
	SkillsDir  string
	// Machine reports the local machine switch for tools that touch this
	// machine. Nil reads machines.json under the bot root; the zero value is
	// allow. off refuses, ask confirms each machine action, allow follows the
	// one policy table.
	Machine func() (string, error)

	// Dispatch offers typed subagents: the main line gets dispatch instead of
	// spawn and the dispatch rule (answer first, hand long work out). It keeps
	// every other tool (ADR 0028).
	Dispatch bool

	// BackgroundApprove asks the owner for a real subagent's confirmation.
	BackgroundApprove agent.Approver

	approvals  approvalState
	envMu      sync.Mutex // serializes environment comparison and publication
	promptOnce sync.Once
	promptEnv  envprompt.Env
	turnMu     sync.Mutex
	operations operationState

	// memWriteMu serializes own memory writes with their before/after reads.
	memWriteMu     sync.Mutex
	startOnce      sync.Once
	legacyForkOnce sync.Once
	recoverOnce    sync.Once
	recoverErr     error
	execOnce       sync.Once
	execMu         sync.RWMutex
	loadMu         sync.Mutex
	loaded         map[string]bool
	linksMu        sync.Mutex
	secretMu       sync.Mutex
	secrets        map[string]map[string]string
	compactMu      sync.Mutex
	compactBusy    bool
	turn           *turn
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
	shellOnce sync.Once
	shell     *tools.Jobs
	shellMu   sync.Mutex
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
	// Live state is memory only; persistContextSnap keeps one cached copy on
	// disk for after a restart. See noteContext.
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
	if k.Resources != nil {
		if !k.Resources.AdmissionOpen() {
			return errors.New("bot resources unavailable")
		}
		_ = k.Resources.MCP.Reconcile(ctx)
	}

	if err := ev.Validate(); err != nil {
		return err
	}

	existing, err := k.findCurrentEvent(ev.ID)
	if err != nil {
		return err
	}
	seq, err := k.receive(ev)
	if err != nil {
		return err
	}
	// Real task owners recover independently of legacy transcript repair.
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
		if handleErr != nil && !errors.Is(handleErr, context.Canceled) {
			handleErr = errors.Join(handleErr, k.replyToFailedInputs(t))
		}
	}()
	t.started = k.now()
	imageGen := k.resolveImageGen(ctx)
	t.imageGen = &imageGen
	k.turnMu.Lock()
	k.turn = t
	k.turnMu.Unlock()
	// The owner can speak into this turn from the moment it exists, so the
	// time spent on setup (compaction, memory) is not a window where a message
	// misses it. A message that arrives before the first model call stops
	// that call before it leaves. Setup failures leave the original words and
	// a normal failure reply, even when their old stream has handed off.
	first := &Sink{OnPart: k.OnPart, OnDelta: k.OnDelta, OnTool: k.OnTool, OnResult: k.OnResult, OnProfile: k.OnProfile, OnHandoff: k.OnHandoff}
	if ev.Source == link.NameTimer {
		first.OnDelta = nil
	}
	k.openInterject(t, first)
	defer func() {
		if unread := k.closeInterject(t, true); len(unread) > 0 {
			t.finishSink(Outcome{Err: errors.New("这一轮出错，插话原文已保留")})
		}
	}()
	defer func() {
		t.mu.Lock()
		t.finished = true
		t.mu.Unlock()
	}()
	k.clearDenials()
	k.clearLoaded()
	// A new wake sends a new context; the panel shows this wake, not the last.
	// The last wake's final state is kept for the inspector after a restart.
	k.resetContext()
	k.persistContextSnap()
	// Swap a finished background summary on the way in and on the way out.
	// Both run outside the model call, including when this event is a replay.
	defer func() {
		cur, loadErr := k.currentState()
		if loadErr != nil {
			return
		}
		_, _, _ = k.applyReadyJob(migrate(cur))
	}()
	// Callers hold the handle lock; legacy repair must not run during openKernel.
	k.RecoverInterrupted()
	if err := k.repairTail(); err != nil {
		return err
	}
	n, err := k.currentCount()
	if err != nil {
		return err
	}
	if existing == 0 && ev.Source != link.NameApproval && link.OwnerTrusted(ev.Source, ev.Sender, k.Bot.Config().Owners) {
		AnswerQuestions(k.Bot, ev.Content)
	}
	if existing > 0 {
		span, err := k.currentSlice(seq, n)
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
	st, err := k.currentState()
	if err != nil {
		return err
	}
	st = migrate(st)
	if key := k.boundModelKey(); key != "" {
		st.BoundModel = key
	}
	// The existing prefix is already the committed snapshot. Do not wait for
	// a background writer merely to read and discard a newer version.
	if st.MemRender != memRenderVersion {
		st.MemSnapSet, st.MemSnap, st.MemRender = false, "", memRenderVersion
	}
	if !st.MemSnapSet {
		memText, err := k.memoryText(context.WithValue(ctx, publishedMemoryRead{}, true), "")
		if err != nil {
			return err
		}
		st.MemSnap, st.MemSnapSet = memText, true
	}
	st, err = k.syncEnv(st, ev, st.MemSnap)
	if err != nil {
		return err
	}
	// The opening message is on the transcript, so this turn now owns this
	// ctx. The synchronous compaction below may summarize, and
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
	// Project attached skills only onto this wake's live input. The raw
	// transcript and fixed prefix retain their original bytes.
	for i := range assembled {
		if assembled[i].Seq == seq && !assembled[i].Internal {
			before := compact.EstimateMessage(assembled[i])
			assembled[i] = k.projectRoutineSkills(assembled[i], ev)
			usage.History += compact.EstimateMessage(assembled[i]) - before
			break
		}
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
		t.projects.noteText(m.Text())
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
		t.projects.noteText(assembled[n-1].Text())
	}
	if n := len(assembled); n > 0 {
		if delta := compact.EstimateMessage(assembled[n-1]) - before; delta > 0 {
			usage.Env += delta
		}
	}
	st.Usage = usage
	if err := k.saveCurrentState(st); err != nil {
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
	// Parked owner grants also work when no live approver is attached.
	approve = k.parkable(t, approve)
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	runCtx = context.WithValue(runCtx, turnKey{}, t)
	t.runCtx = k.withImageGen(runCtx)
	r := &turnRun{
		k:         k,
		t:         t,
		p:         p,
		list:      list,
		assembled: assembled,
		br:        newBreaker(),
	}
	res := r.drive(r.options(approve))
	k.persistContextSnap()
	if r.err != nil {
		return r.err
	}
	if err := k.pushFindings(ctx, res.produced); err != nil {
		return err
	}
	return res.err
}

// absorbInbox appends inbox events that arrived during this turn, oldest first.
// The event being handled stays pending until the caller acks it.
func (k *Kernel) absorbInbox() ([]provider.Message, error) {
	if k.Inbox == nil {
		return nil, nil
	}
	if err := k.reloadCurrentInbox(); err != nil {
		return nil, err
	}
	var out []provider.Message
	for _, ev := range k.pendingCurrentEvents() {
		if ev.ID == "" || ev.ID == k.curTurn().turnEvent {
			continue
		}
		old, err := k.findCurrentEvent(ev.ID)
		if err != nil {
			return nil, err
		}
		if old > 0 && old < k.curTurn().fromSeq {
			n, err := k.currentCount()
			if err != nil {
				return nil, err
			}
			span, err := k.currentSlice(old, n)
			if err != nil {
				return nil, err
			}
			if turnDone(span, 0) {
				if err := k.ackCurrentEvent(ev.ID); err != nil {
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
		if err := k.ackCurrentEvent(ev.ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (k *Kernel) providerFor(ctx context.Context) (provider.Provider, error) {
	if k.Selector != nil {
		p, err := k.Selector.Select(ctx, provider.Hint{Bot: k.currentName()})
		return k.searchProvider(p), err
	}
	if k.Provider == nil {
		return nil, errors.New("kernel: no provider")
	}
	return k.searchProvider(k.Provider), nil
}

// rulesText injects this binary's rules, then the bot's own additions.
func (k *Kernel) rulesText() (string, error) {
	supplements, err := k.botRules()
	if err != nil {
		return "", err
	}
	rules := KernelRules()
	if supplements != "" {
		rules += "\n\n" + supplements
	}
	if k.permissive() {
		rules = strings.Replace(rules, ActionRule, OwnAskActionRule, 1)
		if !strings.Contains(rules, EventMemoryRule) {
			rules += "\n\n" + EventMemoryRule
		}
	}
	if k.dispatching() && !strings.Contains(rules, DispatchRule) {
		rules += "\n\n" + DispatchRule
	}
	if k.hasLink(link.NameMemory) && !strings.Contains(rules, ExperienceRule) {
		rules += "\n\n" + ExperienceRule
	}
	if guide := k.cliProfileGuide(); guide != "" {
		rules += "\n\n" + guide
	}
	if k.Memory == nil || strings.Contains(rules, MemoryWriteRule) {
		return rules, nil
	}
	return rules + "\n\n" + MemoryWriteRule, nil
}

func (k *Kernel) assemble(st bot.State, mem string) ([]provider.Message, bot.SectionUsage, error) {
	persona, err := k.currentComposedPersona()
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
	lines, err := k.currentUncoveredLines(st.Blocks)
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
				e.Message.SetText(q + "\n\n" + e.Message.Text())
			}
		}
		// A pasted picture is named here, on the line that carried it. Only the
		// turn answering that line gets the bytes; see attachEventImages.
		e.Message.SetText(k.withLineNote(e.Message.Role, e.Message.Text(), e.Images, e.Audios))
		raw = append(raw, e)
		seqs = append(seqs, ln.Seq)
	}
	raw, seqs, err = k.reminders().Replay(raw, seqs)
	if err != nil {
		return nil, bot.SectionUsage{}, err
	}
	blobs := map[int]bool{}
	for i, e := range raw {
		if e.Blob != "" {
			blobs[seqs[i]] = true
		}
	}
	msgs, usage := account(Input{
		Persona:   persona,
		Rules:     rules,
		Links:     k.stableLinkNames(),
		Memory:    mem,
		Blocks:    st.Blocks,
		Skills:    skills,
		SkillsDir: k.SkillsDir,
		Installed: installed,
		Overlay:   overlay,
	}, omitPastResults(projected(st.Blocks, raw, seqs), blobs))
	return msgs, usage, nil
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

func (k *Kernel) repairTail() error {
	// Historical branch calls retain their original pairing.
	return k.closeDangling(k.danglingReason())
}

func (k *Kernel) newestReminderSeq(blocks []bot.SummaryBlock) (int, error) {
	if k == nil || k.Bot == nil {
		return 0, nil
	}
	lines, err := k.currentUncoveredLines(blocks)
	if err != nil {
		return 0, err
	}
	return newestSeq(lines), nil
}

func (k *Kernel) pendingNotes() int {
	count := 0
	_ = k.withCurrentIO(func(b *bot.Bot, _ *inbox.Inbox) error {
		notes, err := notify.Open(b.OutboxPath()).Unread()
		count = len(notes)
		return err
	})
	return count
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

const memRenderVersion = 3

// refreshMemSnap publishes the replacement environment before making its covered
// range effective. A crash on either side leaves a replayable conversation;
// there is no second state file to synchronize with it.
func (k *Kernel) refreshMemSnap(ctx context.Context, st *bot.State) error {
	k.envMu.Lock()
	defer k.envMu.Unlock()
	previous, err := k.currentState()
	if err != nil {
		return err
	}
	old, err := k.visibleEnvironment(previous)
	if err != nil {
		return err
	}
	mem, err := k.memoryText(context.WithValue(ctx, publishedMemoryRead{}, true), "")
	if errors.Is(err, memory.ErrBusy) && st.MemSnapSet {
		mem, err = st.MemSnap, nil
	}
	if err != nil {
		return err
	}
	st.MemRender, st.MemSnap, st.MemSnapSet = memRenderVersion, mem, true
	_, err = k.publishEnvironment(*st, old, true, environmentBasis(*st, nil))
	return err
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
		dir := filepath.Join(k.Bot.Root, "bots", k.currentName(), "memory")
		paths[memory.ScopeBot] = shown(filepath.Join(dir, "memory.md")) + "; " + shown(filepath.Join(dir, "log")) + "/"
		if personID != "" {
			paths[memory.ScopePerson] = shown(filepath.Join(k.Bot.Root, "people", personID, "memory", "memory.md"))
		}
	}
	if counter, ok := k.Memory.(interface {
		LogCount(memory.Scope, string, string) (int, error)
	}); ok && ctx.Value(publishedMemoryRead{}) == nil {
		total, err := counter.LogCount(memory.ScopeBot, k.currentName(), "")
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
	botName := k.currentName()
	var personFacts []memory.Fact
	// External senders such as webhook:github are not person ids. Bot memory still loads.
	if memory.ValidID(personID) {
		var err error
		personFacts, err = k.recallMemory(ctx, memory.Query{Scope: memory.ScopePerson, PersonID: personID, Tier: memory.TierProfile})
		if err != nil {
			return nil, nil, err
		}
	}
	botFacts, err := k.recallMemory(ctx, memory.Query{Scope: memory.ScopeBot, BotName: botName, Tier: memory.TierProfile})
	if err != nil {
		return nil, nil, err
	}
	logFacts, err := k.recallMemory(ctx, memory.Query{Scope: memory.ScopeBot, BotName: botName, Tier: memory.TierLog})
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
					return k.postCurrentEvent(ev)
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
	k.retireOperations()
	k.shellMu.Lock()
	jobs := k.shell
	k.shellMu.Unlock()
	if jobs != nil && k.Resources == nil {
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
