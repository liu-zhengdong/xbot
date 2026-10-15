package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPersonFactIsSharedBotPersonIsNot(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	ctx := context.Background()
	when := time.Date(2026, 10, 15, 1, 0, 0, 0, time.UTC)
	err := s.Remember(ctx, Fact{
		Scope: ScopePerson, Tier: TierProfile, PersonID: "u1",
		WrittenBy: "ada", Text: "Ada likes tea", Time: when,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = s.Remember(ctx, Fact{
		Scope: ScopeBotPerson, Tier: TierProfile, BotName: "ada", PersonID: "u1",
		WrittenBy: "ada", Text: "secret handshake", Time: when,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Recall(ctx, Query{Scope: ScopePerson, PersonID: "u1", Tier: TierProfile})
	if err != nil || len(got) != 1 || got[0].Text != "Ada likes tea" {
		t.Fatalf("bot B person view %+v %v", got, err)
	}
	hidden, err := s.Recall(ctx, Query{Scope: ScopeBotPerson, BotName: "bea", PersonID: "u1", Tier: TierProfile})
	if err != nil || len(hidden) != 0 {
		t.Fatalf("other bot saw bot-person %+v %v", hidden, err)
	}
	other, err := s.Recall(ctx, Query{Scope: ScopeBotPerson, BotName: "ada", PersonID: "u2", Tier: TierProfile})
	if err != nil || len(other) != 0 {
		t.Fatalf("other person saw handshake %+v %v", other, err)
	}
	own, err := s.Recall(ctx, Query{Scope: ScopeBotPerson, BotName: "ada", PersonID: "u1", Text: "handshake"})
	if err != nil || len(own) != 1 || own[0].Text != "secret handshake" {
		t.Fatalf("recall %+v %v", own, err)
	}
}

func TestForgetExactAndRejectsBarePersonFact(t *testing.T) {
	s := Open(t.TempDir())
	ctx := context.Background()
	if err := s.Remember(ctx, Fact{Scope: ScopePerson, PersonID: "u1", Text: "no author"}); err == nil {
		t.Fatal("expected attribution error")
	}
	if err := s.Remember(ctx, Fact{
		Scope: ScopeBot, BotName: "ada", Tier: TierLog, Text: "keep a checklist",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Forget(ctx, Query{Scope: ScopeBot, BotName: "ada"}, "keep a checklist"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Recall(ctx, Query{Scope: ScopeBot, BotName: "ada"})
	if err != nil || len(got) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestMarkdownEditRebuildsIndex(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	ctx := context.Background()
	if err := s.Remember(ctx, Fact{
		Scope: ScopePerson, PersonID: "u1", Tier: TierProfile,
		WrittenBy: "ada", Text: "lives in Lisbon",
	}); err != nil {
		t.Fatal(err)
	}
	md := filepath.Join(root, "people", "u1", "memory", "profile.md")
	idx := filepath.Join(root, "people", "u1", "memory", "profile.index.json")
	if err := os.Remove(idx); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(md)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.ReplaceAll(string(raw), "lives in Lisbon", "lives in Porto")
	if err := os.WriteFile(md, []byte(edited), 0600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(md, future, future); err != nil {
		t.Fatal(err)
	}
	got, err := s.Recall(ctx, Query{Scope: ScopePerson, PersonID: "u1", Tier: TierProfile})
	if err != nil || len(got) != 1 || got[0].Text != "lives in Porto" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestSubagentIDIsNotAPerson(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	err := s.Remember(context.Background(), Fact{
		Scope: ScopePerson, PersonID: "sa_abcdef01", WrittenBy: "ada", Text: "nope",
	})
	if err == nil {
		t.Fatal("expected subagent id to be refused")
	}
	if _, statErr := os.Stat(filepath.Join(root, "people", "sa_abcdef01")); !os.IsNotExist(statErr) {
		t.Fatal(statErr)
	}
}

func TestProfileCap(t *testing.T) {
	s := Open(t.TempDir())
	ctx := context.Background()
	for i := 0; i < 80; i++ {
		err := s.Remember(ctx, Fact{
			Scope: ScopeBot, BotName: "ada", Tier: TierProfile,
			Text: strings.Repeat("word ", 80) + string(rune('a'+i%26)),
		})
		if err != nil {
			return
		}
	}
	t.Fatal("expected the profile tier to hit its cap")
}
