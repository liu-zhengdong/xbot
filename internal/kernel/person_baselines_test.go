package kernel

import (
	"context"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

func personBaselineFixture(t *testing.T) (*Kernel, memory.Store) {
	t.Helper()
	b := testBot(t, link.NameTerminal)
	if err := b.SetOwners([]string{"u1", "u2"}); err != nil {
		t.Fatal(err)
	}
	store := memory.Open(b.Root)
	for id, text := range map[string]string{"u1": "ALICE_BASELINE_83", "u2": "BOB_BASELINE_47"} {
		if err := store.Remember(context.Background(), memory.Fact{Scope: memory.ScopePerson, PersonID: id, Tier: memory.TierProfile, Key: "home", Text: text, WrittenBy: id}); err != nil {
			t.Fatal(err)
		}
	}
	return &Kernel{Bot: b, Memory: store}, store
}

// Inspect only the new append-only rows, rather than finding old facts in history.
func receivePersonUpdates(t *testing.T, k *Kernel, ev link.Event) string {
	t.Helper()
	n, err := k.Bot.Count()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.receive(ev); err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	if err := k.Bot.Walk(n+1, 0, func(_ int, e bot.Entry) error {
		if strings.HasPrefix(e.Message.Text(), "<environment_") {
			text.WriteString(e.Message.Text())
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return text.String()
}

func TestPersonBaselinesAlternateWithoutReplayingProfiles(t *testing.T) {
	k, store := personBaselineFixture(t)
	now := time.Unix(1000, 0)
	receivePersonUpdates(t, k, link.TerminalEvent("u1", "alice", now))
	receivePersonUpdates(t, k, link.TerminalEvent("u2", "bob", now))
	// A restart replays both baselines from the effective conversation.
	reloaded, err := bot.Load(k.Bot.Root, k.Bot.Name())
	if err != nil {
		t.Fatal(err)
	}
	k = &Kernel{Bot: reloaded, Memory: store}
	text := receivePersonUpdates(t, k, link.TerminalEvent("u1", "alice again", now))
	if strings.Contains(text, "person_data") || strings.Contains(text, "BASELINE") || !strings.Contains(text, `"sender":"u1"`) {
		t.Fatalf("return replayed an unchanged profile: %s", text)
	}
	if err := store.Remember(context.Background(), memory.Fact{Scope: memory.ScopePerson, PersonID: "u2", Tier: memory.TierProfile, Key: "extra", Text: "BOB_CHANGED_92", WrittenBy: "other"}); err != nil {
		t.Fatal(err)
	}
	text = receivePersonUpdates(t, k, link.TerminalEvent("u2", "bob changed", now))
	if !strings.Contains(text, `"person:u2"`) || !strings.Contains(text, "BOB_CHANGED_92") {
		t.Fatalf("switch must diff against that person's prior version: %s", text)
	}
}

func TestPersonBaselineOwnChangesSurviveOtherSender(t *testing.T) {
	k, store := personBaselineFixture(t)
	now := time.Unix(1000, 0)
	alice := link.TerminalEvent("u1", "alice", now)
	receivePersonUpdates(t, k, alice)
	done := k.beginOwnWrite(context.Background(), memory.ScopePerson, "u1")
	if err := store.Remember(context.Background(), memory.Fact{Scope: memory.ScopePerson, PersonID: "u1", Tier: memory.TierProfile, Key: "own", Text: "OWN_EDIT_64", WrittenBy: k.Bot.Name()}); err != nil {
		t.Fatal(err)
	}
	receipt := done(true)
	call := provider.ToolCall{ID: "own", Function: provider.FunctionCall{Name: "write_file"}}
	if err := k.Bot.Append(bot.Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{call})}}); err != nil {
		t.Fatal(err)
	}
	if err := k.Bot.Append(bot.Entry{Message: provider.Message{Role: provider.RoleTool, Content: provider.ToolResult("own", "ok"+receipt)}}); err != nil {
		t.Fatal(err)
	}
	receivePersonUpdates(t, k, link.TerminalEvent("u2", "bob", now))
	text := receivePersonUpdates(t, k, alice)
	if strings.Contains(text, "OWN_EDIT_64") || strings.Contains(text, "ALICE_BASELINE") {
		t.Fatalf("own change was re-announced after switching people: %s", text)
	}
}

func TestNonPersonSourceDoesNotMentionOrResetProfiles(t *testing.T) {
	k, _ := personBaselineFixture(t)
	now := time.Unix(1000, 0)
	receivePersonUpdates(t, k, link.TerminalEvent("u1", "alice", now))
	ev := link.NewEvent(link.NameTimer, "wake", "timer", "u1", now)
	text := receivePersonUpdates(t, k, ev)
	if !strings.Contains(text, `"source":{"name":"timer"`) || strings.Contains(text, "person:") {
		t.Fatalf("non-person wake must mention only its source: %s", text)
	}
	ev = link.NewEvent(link.NameTimer, "wake", "another timer", "other-routing-id", now)
	text = receivePersonUpdates(t, k, ev)
	if text != "" {
		t.Fatalf("same source emitted an unchanged update: %s", text)
	}
	text = receivePersonUpdates(t, k, link.TerminalEvent("u1", "alice returns", now))
	if strings.Contains(text, "person_data") || strings.Contains(text, "BASELINE") {
		t.Fatalf("non-person wake invalidated the profile: %s", text)
	}
}

func TestPersonBaselinesCompactionRestoresEachPerson(t *testing.T) {
	k, _ := personBaselineFixture(t)
	now := time.Unix(1000, 0)
	receivePersonUpdates(t, k, link.TerminalEvent("u1", "alice", now))
	receivePersonUpdates(t, k, link.TerminalEvent("u2", "bob", now))
	st, err := k.Bot.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	n, _ := k.Bot.Count()
	st.Blocks = []bot.SummaryBlock{{From: 1, To: n, Text: "summary"}}
	// Follow the real compaction publication and save path. An unrelated
	// uncovered environment row must not suppress restoration of the current person.
	if err := k.refreshMemSnap(context.Background(), &st); err != nil {
		t.Fatal(err)
	}
	if err := k.Bot.SaveState(st); err != nil {
		t.Fatal(err)
	}
	if err := k.ensureSnapshot(st); err != nil {
		t.Fatal(err)
	}
	var snapshot string
	if err := k.Bot.Walk(n+1, 0, func(_ int, e bot.Entry) error { snapshot += e.Message.Text(); return nil }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snapshot, "BOB_BASELINE_47") || strings.Contains(snapshot, "ALICE_BASELINE_83") {
		t.Fatalf("compaction did not restore only the current person: %s", snapshot)
	}
	text := receivePersonUpdates(t, k, link.TerminalEvent("u2", "bob again", now))
	if strings.Contains(text, "person_data") {
		t.Fatalf("snapshot-restored profile was repeated: %s", text)
	}
	text = receivePersonUpdates(t, k, link.TerminalEvent("u1", "alice returns", now))
	if !strings.Contains(text, "ALICE_BASELINE_83") || strings.Contains(text, "BOB_BASELINE_47") {
		t.Fatalf("compacted other person's profile was not restored: %s", text)
	}
	text = receivePersonUpdates(t, k, link.TerminalEvent("u2", "bob returns", now))
	if strings.Contains(text, "person_data") {
		t.Fatalf("already restored profile repeated after another person: %s", text)
	}
}
