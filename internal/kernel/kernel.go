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
	// Redact overrides bot.RedactText for transcript text this kernel returns.
	// Nil uses the process replacer.
	Redact  func(string) string
	Desktop func(ctx context.Context, title, body string) error
	// Exec runs subagents. Nil uses in-process goroutines, or a Runner link when one is mounted.
	Exec exec.Executor
	// AllowBackground auto-approves confirm-needed tools inside a subagent.
	// The main chat approver is not used: a background step cannot block on a prompt.
	AllowBackground bool
	WorkDir         string
	SharedDir       string
	SkillsDir       string

	startOnce   sync.Once
	execOnce    sync.Once
	compactMu   sync.Mutex
	compactBusy bool
	turnSender  string
	turnKind    string
	turnHops    int
	turnChain   string
	turnTrusted bool
	turnSource  string
}

// Handle runs one event to a final assistant message, or returns the agent error.
// A repeated event whose turn is already complete does not call the model again.
func (k *Kernel) Handle(ctx context.Context, ev link.Event) error {
	if err := ev.Validate(); err != nil {
		return err
	}
	k.turnSender = ev.Sender
	k.turnKind = ev.SenderKind
	k.turnHops = ev.Hops
	k.turnChain = ev.Chain
	k.turnSource = ev.Source
	k.turnTrusted = link.OwnerTrusted(ev.Source, ev.Sender, k.Bot.Config().Owners)
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
	if seq == 0 {
		entry := bot.Entry{
			Time:    ev.Time,
			EventID: ev.ID,
			Message: provider.Message{Role: provider.RoleUser, Content: link.Render(ev, k.turnTrusted)},
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
	_, err = agent.Run(ctx, p, list, assembled, agent.Options{
		MaxSteps: k.MaxSteps,
		Approve:  k.Approve,
		OnDelta:  onDelta,
		OnTool:   k.OnTool,
		OnResult: k.OnResult,
		OnMessage: func(m provider.Message) {
			if appendErr != nil {
				return
			}
			appendErr = k.Bot.Append(bot.Entry{Time: k.now(), Message: m})
		},
	})
	if appendErr != nil {
		return appendErr
	}
	return err
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
	raw, err := k.Bot.Uncovered(st.Blocks)
	if err != nil {
		return nil, bot.SectionUsage{}, err
	}
	msgs, usage := account(Input{
		Persona: persona,
		Rules:   rules,
		Links:   k.linkNames(),
		Memory:  mem,
		Blocks:  st.Blocks,
		Skills:  skills,
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
	now := k.now()
	next := bot.EnvRecord{
		Hour:    now.Format("2006-01-02T15"),
		Sender:  ev.Sender,
		Tools:   toolNames(list),
		Notes:   k.pendingNotes(),
		Persona: shortHash(persona),
		Rules:   shortHash(rules),
		Memory:  shortHash(mem),
		Skills:  skillHash,
		Profile: profileHash,
		At:      now,
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
	missing, err := unresolvedTail(span)
	if err != nil {
		return err
	}
	for _, tc := range missing {
		if err := k.Bot.Append(placeholder(tc)); err != nil {
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
// A bot or group principal must not be looked up as someone else's memory.
func (k *Kernel) memoryPerson() string {
	if k.turnKind == link.KindBot || k.turnKind == link.KindGroup {
		return ""
	}
	return k.turnSender
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
		for _, l := range k.Links {
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
			approved, err = k.Approve(ctx, m.Name(), preview)
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
