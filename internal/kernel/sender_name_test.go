package kernel

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/person"
	"xbot/internal/provider"
)

func TestEnvironmentSenderUsesPersonName(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	who, err := person.Identify(b.Root, person.Account{Link: link.NameTerminal, ExternalID: "uid:501"}, "刘政东")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{who.ID}); err != nil {
		t.Fatal(err)
	}
	store := memory.Open(b.Root)
	ctx := context.Background()
	if err := store.Remember(ctx, memory.Fact{Scope: memory.ScopePerson, PersonID: who.ID, Tier: memory.TierProfile, Key: "home", Text: "PERSON_FACT", WrittenBy: who.ID}); err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("hello")}}}
	k := &Kernel{Bot: b, Provider: p, Memory: store, MaxSteps: 2}
	if err := k.Handle(ctx, link.TerminalEvent(who.ID, "hello", time.Now())); err != nil {
		t.Fatal(err)
	}
	check := func(e environment, name string) {
		t.Helper()
		var source map[string]any
		if err := json.Unmarshal(e.Fields["source"], &source); err != nil {
			t.Fatal(err)
		}
		if source["sender"] != name || source["sender_id"] != who.ID {
			t.Fatalf("source = %v, want sender=%s sender_id=%s", source, name, who.ID)
		}
		if !strings.Contains(environmentGet[string](e, "person:"+who.ID), "PERSON_FACT") {
			t.Fatal("named sender lost person memory")
		}
	}
	check(replayEnvironment(p.reqs[0].Messages, ""), who.DisplayName)
	before := testPublished(t, k)
	who.DisplayName = "政东"
	if err := person.Record(b.Root, "", "", time.Time{}, who); err != nil {
		t.Fatal(err)
	}
	next, err := k.sampleEnvironment(before)
	if err != nil {
		t.Fatal(err)
	}
	check(next, who.DisplayName)
	// Old conversations carried only the id in sender. Sampling upgrades the
	// source in the next appended update, without rewriting previous entries.
	environmentSet(&before, "source", map[string]any{"name": "terminal", "sender": who.ID, "input": 1})
	next, err = k.sampleEnvironment(before)
	if err != nil {
		t.Fatal(err)
	}
	check(next, who.DisplayName)
}
