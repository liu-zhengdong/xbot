// Package memory defines layered memory. M1 has the interfaces and the
// conflict rule only. Nothing is stored or retrieved yet; M2 fills the store.
//
// Scopes:
//   - person: facts about a person, shared by every bot. Each fact records which bot wrote it.
//   - bot: this bot's role, methods, and topic context.
//   - bot-person: agreements between this bot and this person. Invisible to other people.
//
// Conflict rule:
//   - Facts about the person (KindAttribute): the newest fact for a key wins.
//   - How to work (KindWork): bot scope wins over person scope and bot-person scope.
package memory

import (
	"context"
	"errors"
	"time"
)

// Scope is where a fact lives.
type Scope string

const (
	ScopePerson    Scope = "person"
	ScopeBot       Scope = "bot"
	ScopeBotPerson Scope = "bot-person"
)

// Kind distinguishes person attributes from working agreements.
type Kind string

const (
	// KindAttribute is a fact about the person. Newest wins.
	KindAttribute Kind = "attribute"
	// KindWork is how the bot should work. Bot scope wins.
	KindWork Kind = "work"
)

// Fact is one memory record. Retrieval is M2; the shape is fixed in M1.
type Fact struct {
	Key       string
	Kind      Kind
	Scope     Scope
	PersonID  string
	BotName   string
	WrittenBy string
	Text      string
	Time      time.Time
}

// Query selects facts for context assembly. A store must not return
// bot-person facts for a different person than PersonID.
type Query struct {
	Scope    Scope
	BotName  string
	PersonID string
}

// Store is the M2 persistence seam. Context assembly calls Recall.
// M1's NopStore recalls nothing.
type Store interface {
	Remember(ctx context.Context, fact Fact) error
	Recall(ctx context.Context, q Query) ([]Fact, error)
}

// ErrNotImplemented is returned by the M1 store. Remember is not wired to the model.
var ErrNotImplemented = errors.New("memory: retrieval is not implemented")

// NopStore satisfies Store without storing or recalling facts.
type NopStore struct{}

func (NopStore) Remember(context.Context, Fact) error { return ErrNotImplemented }

func (NopStore) Recall(context.Context, Query) ([]Fact, error) { return nil, nil }

// Visible drops facts the current bot and sender must not see.
// Bot-person facts for anyone else are removed. This is a backstop behind Recall.
func Visible(facts []Fact, botName, personID string) []Fact {
	out := make([]Fact, 0, len(facts))
	for _, f := range facts {
		switch f.Scope {
		case ScopePerson:
			if f.PersonID == personID {
				out = append(out, f)
			}
		case ScopeBot:
			if f.BotName == botName {
				out = append(out, f)
			}
		case ScopeBotPerson:
			if f.BotName == botName && f.PersonID == personID {
				out = append(out, f)
			}
		}
	}
	return out
}

// Resolve applies the conflict rule. The result has at most one fact per key.
func Resolve(facts []Fact) []Fact {
	type slot struct {
		fact Fact
		ok   bool
	}
	order := make([]string, 0)
	slots := map[string]*slot{}
	work := map[string][]Fact{}
	attr := map[string][]Fact{}
	for _, f := range facts {
		if f.Key == "" {
			continue
		}
		if _, ok := slots[f.Key]; !ok {
			slots[f.Key] = &slot{}
			order = append(order, f.Key)
		}
		switch f.Kind {
		case KindWork:
			work[f.Key] = append(work[f.Key], f)
		default:
			attr[f.Key] = append(attr[f.Key], f)
		}
	}
	var out []Fact
	for _, key := range order {
		if ws := work[key]; len(ws) > 0 {
			out = append(out, newest(preferBot(ws)))
			continue
		}
		if as := attr[key]; len(as) > 0 {
			out = append(out, newest(as))
		}
	}
	return out
}

func preferBot(facts []Fact) []Fact {
	var botScoped []Fact
	for _, f := range facts {
		if f.Scope == ScopeBot {
			botScoped = append(botScoped, f)
		}
	}
	if len(botScoped) > 0 {
		return botScoped
	}
	return facts
}

func newest(facts []Fact) Fact {
	best := facts[0]
	for _, f := range facts[1:] {
		if !f.Time.Before(best.Time) {
			best = f
		}
	}
	return best
}
