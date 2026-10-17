package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPersonFactIsSharedBotFactIsNot(t *testing.T) {
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
		Scope: ScopeBot, Tier: TierProfile, BotName: "ada",
		WrittenBy: "ada", Text: "secret handshake", Time: when,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Remember(ctx, Fact{
		Scope: ScopeBotPerson, BotName: "ada", PersonID: "u1", WrittenBy: "ada", Text: "nope",
	}); err == nil {
		t.Fatal("bot-person scope was accepted")
	}
	got, err := s.Recall(ctx, Query{Scope: ScopePerson, PersonID: "u1", Tier: TierProfile})
	if err != nil || len(got) != 1 || got[0].Text != "Ada likes tea" {
		t.Fatalf("shared person view %+v %v", got, err)
	}
	other, err := s.Recall(ctx, Query{Scope: ScopeBot, BotName: "bea", Tier: TierProfile})
	if err != nil || len(other) != 0 {
		t.Fatalf("other bot saw ada's memory %+v %v", other, err)
	}
	own, err := s.Recall(ctx, Query{Scope: ScopeBot, BotName: "ada", Text: "handshake"})
	if err != nil || len(own) != 1 || own[0].Text != "secret handshake" {
		t.Fatalf("recall %+v %v", own, err)
	}
}

func TestOmittedScopeWritesBot(t *testing.T) {
	s := Open(t.TempDir())
	ctx := context.Background()
	if err := s.Remember(ctx, Fact{
		BotName: "ada", WrittenBy: "ada", Tier: TierProfile, Text: "run tests before done",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Recall(ctx, Query{Scope: ScopeBot, BotName: "ada", Tier: TierProfile})
	if err != nil || len(got) != 1 || got[0].Text != "run tests before done" || got[0].Scope != ScopeBot {
		t.Fatalf("bot %+v %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(s.Root, "people")); !os.IsNotExist(err) {
		t.Fatalf("omitted scope wrote person memory: %v", err)
	}
	if err := s.Forget(ctx, Query{BotName: "ada"}, "run tests before done"); err != nil {
		t.Fatal(err)
	}
	got, err = s.Recall(ctx, Query{Scope: ScopeBot, BotName: "ada"})
	if err != nil || len(got) != 0 {
		t.Fatalf("forget omitted scope %+v %v", got, err)
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
	var hit error
	for i := 0; i < 80; i++ {
		err := s.Remember(ctx, Fact{
			Scope: ScopeBot, BotName: "ada", Tier: TierProfile,
			Text: strings.Repeat("word ", 80) + string(rune('a'+i%26)),
		})
		if err != nil {
			hit = err
			break
		}
	}
	if hit == nil {
		t.Fatal("expected the profile tier to hit its cap")
	}
	msg := hit.Error()
	for _, part := range []string{"Consolidate", "merge facts", "forget stale", "log tier"} {
		if !strings.Contains(msg, part) {
			t.Fatalf("cap error %q missing %q", msg, part)
		}
	}
	md, err := os.ReadFile(filepath.Join(s.Root, "bots", "ada", "memory", "profile.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(md) > ProfileBytes {
		t.Fatalf("profile file is %d bytes, over the %d cap", len(md), ProfileBytes)
	}
	got, err := s.Recall(ctx, Query{Scope: ScopeBot, BotName: "ada", Tier: TierProfile})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || len(got) >= 80 {
		t.Fatalf("stored %d facts after a rejected remember", len(got))
	}
}

// overCapProfile writes a person profile larger than ProfileBytes straight to
// disk, as files written before the cap existed are, and returns its facts.
func overCapProfile(t *testing.T, root, id string) []Fact {
	t.Helper()
	var facts []Fact
	for i := 0; i < 20; i++ {
		facts = append(facts, Fact{
			Key: fmt.Sprintf("k%02d", i), WrittenBy: "diary",
			Text: fmt.Sprintf("fact %02d %s", i, strings.Repeat("x", 300)),
			Time: time.Date(2026, 10, 1, 0, i, 0, 0, time.UTC),
		})
	}
	body := renderMD(append([]Fact(nil), facts...))
	if len(body) <= ProfileBytes {
		t.Fatalf("fixture is only %d bytes", len(body))
	}
	dir := filepath.Join(root, "people", id, "memory")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "profile.md"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return facts
}

// A profile already over the cap must accept forgets one at a time, or the
// bot can never consolidate it. Before the fix every write checked the whole
// file against the cap, so this forget failed with "profile tier is full".
func TestForgetOnOverCapProfileSucceeds(t *testing.T) {
	root := t.TempDir()
	facts := overCapProfile(t, root, "u1000")
	s := Open(root)
	ctx := context.Background()
	q := Query{Scope: ScopePerson, PersonID: "u1000", Tier: TierProfile}
	if err := s.Forget(ctx, q, facts[0].Text); err != nil {
		t.Fatalf("forget on an over-cap profile: %v", err)
	}
	got, err := s.Recall(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(facts)-1 {
		t.Fatalf("got %d facts, want %d", len(got), len(facts)-1)
	}
}

func TestRememberOnOverCapProfileStillErrors(t *testing.T) {
	root := t.TempDir()
	overCapProfile(t, root, "u1000")
	before, _ := os.ReadFile(filepath.Join(root, "people", "u1000", "memory", "profile.md"))
	err := Open(root).Remember(context.Background(), Fact{
		Scope: ScopePerson, PersonID: "u1000", Tier: TierProfile, WrittenBy: "diary", Text: "one more fact",
	})
	if err == nil {
		t.Fatal("remember on an over-cap profile should fail")
	}
	for _, part := range []string{"Consolidate", "merge facts", "forget stale", "log tier"} {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("cap error %q missing %q", err, part)
		}
	}
	after, _ := os.ReadFile(filepath.Join(root, "people", "u1000", "memory", "profile.md"))
	if string(before) != string(after) {
		t.Fatal("a rejected remember changed the file")
	}
}
