// Package kernel wakes a bot for one event. It knows the inbox's events and the
// granted tools. It does not know which link produced the event.
package kernel

import (
	"context"
	"fmt"
	"sync"
	"time"

	"xbot/internal/agent"
	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/memory"
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

	startOnce sync.Once
}

// Handle runs one event to a final assistant message, or returns the agent error.
// A repeated event whose turn is already complete does not call the model again.
func (k *Kernel) Handle(ctx context.Context, ev link.Event) error {
	ev.Untrusted = true
	if err := ev.Validate(); err != nil {
		return err
	}
	entries, err := k.Bot.Entries()
	if err != nil {
		return err
	}
	entries, err = k.repair(entries)
	if err != nil {
		return err
	}
	idx := indexEvent(entries, ev.ID)
	if idx >= 0 && turnDone(entries, idx) {
		if k.OnDelta != nil {
			if text := lastAssistant(entries, idx); text != "" {
				k.OnDelta(text)
			}
		}
		return nil
	}
	if idx < 0 {
		entry := bot.Entry{
			Time:    ev.Time,
			EventID: ev.ID,
			Message: provider.Message{Role: provider.RoleUser, Content: link.Wrap(ev)},
		}
		if err := k.Bot.Append(entry); err != nil {
			return err
		}
		entries = append(entries, entry)
	}
	msgs := Messages(entries)
	state, err := k.Bot.LoadState()
	if err != nil {
		return err
	}
	cst := compact.State{Summary: state.Summary, Covered: state.Covered}
	cst, err = compact.Maybe(ctx, k.Compactor, k.Budget, cst, msgs)
	if cst.Summary != state.Summary || cst.Covered != state.Covered {
		if saveErr := k.Bot.SaveState(bot.State{Summary: cst.Summary, Covered: cst.Covered}); saveErr != nil {
			return saveErr
		}
	}
	if err != nil {
		return err
	}
	if cst.Covered > len(msgs) {
		return fmt.Errorf("kernel: covered %d past transcript %d", cst.Covered, len(msgs))
	}
	facts, err := k.loadMemory(ctx, ev.Sender)
	if err != nil {
		return err
	}
	assembled := Assemble(Input{
		Persona:  k.Bot.Config().Persona,
		SenderID: ev.Sender,
		Summary:  cst.Summary,
		Facts:    facts,
		Recent:   msgs[cst.Covered:],
	})
	var appendErr error
	_, err = agent.Run(ctx, k.Provider, link.CollectTools(k.Links), assembled, agent.Options{
		MaxSteps: k.MaxSteps,
		Approve:  k.Approve,
		OnDelta:  k.OnDelta,
		OnTool:   k.OnTool,
		OnResult: k.OnResult,
		OnMessage: func(m provider.Message) {
			if appendErr != nil {
				return
			}
			appendErr = k.Bot.Append(bot.Entry{Time: time.Now().UTC(), Message: m})
		},
	})
	if appendErr != nil {
		return appendErr
	}
	return err
}

func (k *Kernel) repair(entries []bot.Entry) ([]bot.Entry, error) {
	missing, err := unresolvedTail(entries)
	if err != nil {
		return nil, err
	}
	for _, tc := range missing {
		entry := placeholder(tc)
		if err := k.Bot.Append(entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// loadMemory is the context-assembly seam for the three memory scopes.
// M1's store recalls nothing. M2 returns facts; Visible and Resolve still apply.
func (k *Kernel) loadMemory(ctx context.Context, personID string) ([]memory.Fact, error) {
	if k.Memory == nil {
		return nil, nil
	}
	botName := k.Bot.Name()
	personFacts, err := k.Memory.Recall(ctx, memory.Query{Scope: memory.ScopePerson, PersonID: personID})
	if err != nil {
		return nil, err
	}
	botFacts, err := k.Memory.Recall(ctx, memory.Query{Scope: memory.ScopeBot, BotName: botName})
	if err != nil {
		return nil, err
	}
	relFacts, err := k.Memory.Recall(ctx, memory.Query{Scope: memory.ScopeBotPerson, BotName: botName, PersonID: personID})
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
