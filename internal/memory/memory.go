// Package memory defines layered memory and the file store behind it.
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

// Tier is how a fact is loaded. Profile is small and always injected.
// Log is dated and returned by recall, not by the standing context.
type Tier string

const (
	TierProfile Tier = "profile"
	TierLog     Tier = "log"
)

// ProfileBytes is the write cap for one profile-tier file.
// Person, bot, and bot-person each have their own file, and standing
// context renders all three together, so ProfileInjectBytes fits three
// full files plus section headings. A fact the store accepted is not
// dropped on the way in.
const (
	ProfileBytes       = 4 << 10
	profileInjectSlack = 512
	ProfileInjectBytes = 3*ProfileBytes + profileInjectSlack
)

// Fact is one memory record.
type Fact struct {
	Key       string `json:"key"`
	Kind      Kind   `json:"kind"`
	Scope     Scope  `json:"scope"`
	Tier      Tier   `json:"tier,omitempty"`
	PersonID  string `json:"person_id,omitempty"`
	BotName   string `json:"bot_name,omitempty"`
	WrittenBy string `json:"written_by,omitempty"`
	// Device is the node that wrote the fact. Empty means this machine.
	Device string `json:"device,omitempty"`
	// Untrusted is set for a fact that arrived from another node or an untrusted turn.
	Untrusted bool `json:"untrusted,omitempty"`
	// OwnerRecorded is a fact the owner wrote on another of their linked devices.
	// It is still data, not an instruction.
	OwnerRecorded bool      `json:"owner_recorded,omitempty"`
	Text          string    `json:"text"`
	Time          time.Time `json:"time"`
}

// Query selects facts. An empty Tier does not filter. Text is a keyword query.
type Query struct {
	Scope    Scope
	BotName  string
	PersonID string
	Tier     Tier
	Text     string
}

// Store persists the three scopes. Forget removes an exact fact text.
type Store interface {
	Remember(ctx context.Context, fact Fact) error
	Recall(ctx context.Context, q Query) ([]Fact, error)
	Forget(ctx context.Context, q Query, exactText string) error
}

// ErrNotImplemented is returned by the M1 store. Remember is not wired to the model.
var ErrNotImplemented = errors.New("memory: retrieval is not implemented")

// NopStore satisfies Store without storing or recalling facts.
type NopStore struct{}

func (NopStore) Remember(context.Context, Fact) error { return ErrNotImplemented }

func (NopStore) Recall(context.Context, Query) ([]Fact, error) { return nil, nil }

func (NopStore) Forget(context.Context, Query, string) error { return ErrNotImplemented }

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
