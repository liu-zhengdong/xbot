// Package kernel wakes a bot for one event. It knows the inbox's events and the
// granted tools. It does not know which link produced the event.
package kernel

import (
	"context"
	"errors"
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
	Vault   *vault.Store
	Desktop func(ctx context.Context, title, body string) error
	// Exec runs subagents. Nil uses in-process goroutines, or a Runner link when one is mounted.
	Exec exec.Executor
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

	turnMu      sync.Mutex
	bgMu        sync.Mutex
	bgs         map[string]*turn
	finished    []BackgroundInfo
	startOnce   sync.Once
	execOnce    sync.Once
	loadMu      sync.Mutex
	loaded      map[string]bool
	linksMu     sync.Mutex
	secretMu    sync.Mutex
	secrets     map[string]map[string]string
	compactMu   sync.Mutex
	compactBusy bool
	turn        *turn
}

// Handle runs one event to a final assistant message, or returns the agent error.
// A repeated event whose turn is already complete does not call the model again.
func (k *Kernel) Handle(ctx context.Context, ev link.Event) error {
	if err := ev.Validate(); err != nil {
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
	t.started = k.now()
	t.task = clipRunes(strings.TrimSpace(ev.Content), forkTaskRunes)
	t.forked = make(chan struct{})
	k.turnMu.Lock()
	k.turn = t
	k.turnMu.Unlock()
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
	memText, err := k.memoryText(ctx, k.memoryPerson())
	if err != nil {
		return err
	}
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
	st, err = k.autoCompact(ctx, st, seq, memText)
	if err != nil {
		return err
	}
	k.maybeStartBackground(st, seq, memText)
	if err := k.ensureSnapshot(st); err != nil {
		return err
	}
	assembled, usage, err := k.assemble(st, memText)
	if err != nil {
		return err
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
	opts := agent.Options{
		MaxSteps: k.MaxSteps,
		Approve:  approve,
		OnDelta: func(s string) {
			if onDelta != nil && !t.isForked() {
				onDelta(s)
			}
		},
		OnTool: func(name, summary string) {
			t.mu.Lock()
			t.tools++
			t.lastTool = name
			forked := t.branch != ""
			t.mu.Unlock()
			if k.OnTool != nil && !forked {
				k.OnTool(name, summary)
			}
		},
		OnResult: func(name, output string) {
			if k.OnResult != nil && !t.isForked() {
				k.OnResult(name, output)
			}
		},
		Redact: k.redactor(),
		OnMessage: func(m provider.Message) {
			if appendErr != nil {
				return
			}
			appendErr = k.recordTurn(t, m)
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
		rp := &replyProvider{inner: p, accept: k.userReplyTarget}
		produced, err := agent.Run(runCtx, rp, list, assembled, opts)
		var bg bool
		for {
			if err == nil {
				err = appendErr
			}
			// A queued control that arrived after the last step starts another run, so a
			// background works through its queue before it ends.
			t.mu.Lock()
			bg = t.branch != ""
			var pend []provider.Message
			if err == nil && bg {
				pend = t.takeCtrlLocked()
			}
			if err != nil || !bg || len(pend) == 0 {
				t.finished = true
				t.mu.Unlock()
				break
			}
			t.mu.Unlock()
			convo := append(append([]provider.Message(nil), assembled...), produced...)
			for _, m := range pend {
				if rerr := k.recordTurn(t, m); rerr != nil && appendErr == nil {
					appendErr = rerr
				}
				convo = append(convo, m)
				produced = append(produced, m)
			}
			more, rerr := agent.Run(runCtx, rp, list, convo, opts)
			produced = append(produced, more...)
			err = rerr
		}
		if bg {
			k.finishBackground(t, produced, err)
			cancelRun()
			return
		}
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
	raw := make([]bot.Entry, 0, len(lines))
	for _, ln := range lines {
		e := ln.Entry
		if e.ReplyTo > 0 && e.Message.Role == provider.RoleUser {
			if q := k.quotePrefix(e.ReplyTo); q != "" {
				e.Message.Content = q + "\n\n" + e.Message.Content
			}
		}
		raw = append(raw, e)
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
	now := k.now()
	next := bot.EnvRecord{
		Hour:        now.Format("2006-01-02T15"),
		Now:         k.localStamp(),
		Sender:      ev.Sender,
		Tools:       toolNames(list),
		Notes:       k.pendingNotes(),
		Persona:     shortHash(persona),
		Rules:       shortHash(rules),
		Memory:      shortHash(mem),
		Skills:      skillHash,
		Profile:     profileHash,
		OverlayKey:  overlayKey,
		OverlayHash: overlayHash,
		OverlayBody: overlay,
		At:          now,
	}
	diff := DiffEnv(st.Env, next)
	st.Env = next
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
	open, err := k.Bot.OpenTools()
	if err != nil || len(open) == 0 {
		return err
	}
	minSeq := open[0].CallSeq
	for _, t := range open[1:] {
		if t.CallSeq < minSeq {
			minSeq = t.CallSeq
		}
	}
	n, err := k.Bot.Count()
	if err != nil {
		return err
	}
	span, err := k.Bot.Slice(minSeq, n)
	if err != nil {
		return err
	}
	missing, branches, err := unresolvedCalls(span, minSeq)
	if err != nil {
		return err
	}
	// A call on a background still running here is in flight, not lost.
	k.bgMu.Lock()
	var lost []provider.ToolCall
	for _, tc := range missing {
		if _, running := k.bgs[branches[tc.ID]]; !running {
			lost = append(lost, tc)
		}
	}
	k.bgMu.Unlock()
	missing = lost
	for _, tc := range missing {
		e := placeholder(tc)
		e.Branch = branches[tc.ID]
		if err := k.Bot.Append(e); err != nil {
			return err
		}
	}
	return nil
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
	t := k.curTurn()
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

func (k *Kernel) memoryText(ctx context.Context, personID string) (string, error) {
	facts, err := k.loadMemory(ctx, personID)
	if err != nil {
		return "", err
	}
	return clipMemory(renderMemory(facts)), nil
}

// loadMemory is the context-assembly seam for the three memory scopes.
// M1's store recalls nothing. M2 returns facts; Visible and Resolve still apply.
func (k *Kernel) loadMemory(ctx context.Context, personID string) ([]memory.Fact, error) {
	if k.Memory == nil {
		return nil, nil
	}
	botName := k.Bot.Name()
	var personFacts, relFacts []memory.Fact
	// External senders such as webhook:github are not person ids. Bot memory still loads.
	if memory.ValidID(personID) {
		var err error
		personFacts, err = k.Memory.Recall(ctx, memory.Query{Scope: memory.ScopePerson, PersonID: personID, Tier: memory.TierProfile})
		if err != nil {
			return nil, err
		}
		relFacts, err = k.Memory.Recall(ctx, memory.Query{Scope: memory.ScopeBotPerson, BotName: botName, PersonID: personID, Tier: memory.TierProfile})
		if err != nil {
			return nil, err
		}
	}
	botFacts, err := k.Memory.Recall(ctx, memory.Query{Scope: memory.ScopeBot, BotName: botName, Tier: memory.TierProfile})
	if err != nil {
		return nil, err
	}
	all := append(append(personFacts, botFacts...), relFacts...)
	return memory.Resolve(memory.Visible(all, botName, personID)), nil
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
