package memory

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNopStore(t *testing.T) {
	var s NopStore
	if err := s.Remember(context.Background(), Fact{}); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("remember: %v", err)
	}
	got, err := s.Recall(context.Background(), Query{Scope: ScopePerson, PersonID: "u1"})
	if err != nil || got != nil {
		t.Fatalf("recall: %v %v", got, err)
	}
}

func TestVisibleHidesOtherPeople(t *testing.T) {
	facts := []Fact{
		{Key: "city", Scope: ScopePerson, PersonID: "u1", Text: "a"},
		{Key: "city", Scope: ScopePerson, PersonID: "u2", Text: "b"},
		{Key: "tone", Scope: ScopeBot, BotName: "ada", Text: "terse"},
		{Key: "tone", Scope: ScopeBot, BotName: "other", Text: "loud"},
		{Key: "old", Scope: ScopeBotPerson, BotName: "ada", PersonID: "u1", Text: "hidden-scope"},
	}
	got := Visible(facts, "ada", "u1")
	if len(got) != 2 {
		t.Fatalf("got %d facts: %+v", len(got), got)
	}
	for _, f := range got {
		if f.PersonID == "u2" || f.BotName == "other" {
			t.Fatalf("leaked %+v", f)
		}
	}
}

func TestResolveNewestPersonFact(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	facts := []Fact{
		{Key: "city", Kind: KindAttribute, Scope: ScopePerson, PersonID: "u1", Text: "old", Time: t0, WrittenBy: "a"},
		{Key: "city", Kind: KindAttribute, Scope: ScopePerson, PersonID: "u1", Text: "new", Time: t0.Add(time.Hour), WrittenBy: "b"},
	}
	got := Resolve(facts)
	if len(got) != 1 || got[0].Text != "new" {
		t.Fatalf("%+v", got)
	}
}

func TestResolveWorkBotScopeWins(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	facts := []Fact{
		{Key: "style", Kind: KindWork, Scope: ScopePerson, PersonID: "u1", Text: "emoji", Time: t0.Add(3 * time.Hour)},
		{Key: "style", Kind: KindWork, Scope: ScopeBot, BotName: "ada", Text: "terse", Time: t0},
		{Key: "style", Kind: KindWork, Scope: ScopeBot, BotName: "ada", Text: "terse-v2", Time: t0.Add(time.Hour)},
	}
	got := Resolve(facts)
	if len(got) != 1 || got[0].Text != "terse-v2" || got[0].Scope != ScopeBot {
		t.Fatalf("%+v", got)
	}
}

func TestResolveWorkWithoutBotScopeUsesNewest(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	facts := []Fact{
		{Key: "hours", Kind: KindWork, Scope: ScopePerson, Text: "old", Time: t0},
		{Key: "hours", Kind: KindWork, Scope: ScopePerson, Text: "new", Time: t0.Add(time.Minute)},
	}
	got := Resolve(facts)
	if len(got) != 1 || got[0].Text != "new" {
		t.Fatalf("%+v", got)
	}
}
