package kernel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

// These black-box regressions compile against the previous implementation too;
// the negative run changes no production file or acceptance threshold.
func envBaselineKernel(t *testing.T) (*Kernel, *scripted) {
	t.Helper()
	k, store := personBaselineFixture(t)
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "one"}, {Role: provider.RoleAssistant, Content: "two"}}}
	k.Memory, k.Provider, k.MaxSteps, k.Now = store, p, 2, func() time.Time { return time.Unix(1000, 0) }
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "first", time.Unix(1000, 0))); err != nil {
		t.Fatal(err)
	}
	return k, p
}
func rewriteEnvironmentState(t *testing.T, k *Kernel, value any) {
	t.Helper()
	path := filepath.Join(bot.Dir(k.Bot.Root, k.Bot.Name()), "state.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	if value == nil {
		delete(state, "env")
	} else {
		state["env"] = value
	}
	raw, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestEnvironmentIgnoresContradictoryLegacyLedger(t *testing.T) {
	k, _ := envBaselineKernel(t)
	rewriteEnvironmentState(t, k, map[string]any{"sender": "u1", "source": "terminal", "person_active": true, "at": time.Unix(1000, 0), "people": map[string]any{"u1": map[string]any{"text": "WRONG_PERSON", "seen": true}}})
	text := receivePersonUpdates(t, k, link.TerminalEvent("u1", "again", time.Unix(1000, 0)))
	if text != "" {
		t.Fatalf("shadow ledger changed replayed baseline: %s", text)
	}
}
func TestEnvironmentRestartUsesConversationOnly(t *testing.T) {
	k, _ := envBaselineKernel(t)
	rewriteEnvironmentState(t, k, nil)
	reloaded, err := bot.Load(k.Bot.Root, k.Bot.Name())
	if err != nil {
		t.Fatal(err)
	}
	restarted := &Kernel{Bot: reloaded, Memory: k.Memory}
	text := receivePersonUpdates(t, restarted, link.TerminalEvent("u1", "again", time.Unix(1000, 0)))
	if text != "" {
		t.Fatalf("restart re-announced published person: %s", text)
	}
}
func publishedSnapshot(text string) bool {
	if !strings.HasPrefix(text, "<environment_update>\n") {
		return false
	}
	var p struct {
		Snapshot string `json:"snapshot"`
	}
	body := strings.TrimSuffix(strings.TrimPrefix(text, "<environment_update>\n"), "\n</environment_update>")
	return json.Unmarshal([]byte(body), &p) == nil && p.Snapshot != ""
}
func TestEnvironmentCompressionRestoresFullDespiteUnrelatedUpdate(t *testing.T) {
	k, _ := envBaselineKernel(t)
	k.Compactor = &recCompactor{}
	k.Budget.KeepTail = 1
	k.bgs = map[string]*turn{"bg_live": {branch: "bg_live", task: "UNFINISHED_TASK"}}
	st0, err := k.Bot.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.syncEnv(st0, link.Event{}, ""); err != nil {
		t.Fatal(err)
	}
	n, _ := k.Bot.Count()
	// Retain a kernel update unrelated to the removed person/task baseline.
	if err := k.Bot.Append(bot.Entry{Origin: bot.OriginInternal, Message: provider.Message{Role: provider.RoleUser, Content: "<environment_update>\nnow: later\n</environment_update>"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := k.CompactRange(context.Background(), 1, n, "summary without person or task"); err != nil {
		t.Fatal(err)
	}
	st, err := k.Bot.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if err := k.ensureSnapshot(st); err != nil {
		t.Fatal(err)
	}
	visible, err := k.Bot.Uncovered(st.Blocks)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, e := range visible {
		if publishedSnapshot(e.Message.Content) {
			found++
			if !strings.Contains(e.Message.Content, "ALICE_BASELINE_83") || !strings.Contains(e.Message.Content, "UNFINISHED_TASK") {
				t.Fatalf("snapshot incomplete: %s", e.Message.Content)
			}
		}
	}
	if found != 1 {
		t.Fatalf("got %d full snapshots, want one", found)
	}
	before, _ := k.Bot.Count()
	if err := k.ensureSnapshot(st); err != nil {
		t.Fatal(err)
	}
	after, _ := k.Bot.Count()
	if after != before {
		t.Fatal("full snapshot repeated without change")
	}
}
func TestEnvironmentInlineFoldRestoresLiveSnapshot(t *testing.T) {
	p := &scripted{resps: longTurn(18, 2000)}
	k, _ := fitKernel(t, p, &recCompactor{}, compact.Budget{Ceiling: 12000, KeepTail: 1}, 8000)
	k.Memory = memory.Open(k.Bot.Root)
	if err := k.Memory.Remember(context.Background(), memory.Fact{Scope: memory.ScopePerson, PersonID: "u1", Tier: memory.TierProfile, Key: "name", Text: "PERSON_IN_FOLD_49", WrittenBy: "u1"}); err != nil {
		t.Fatal(err)
	}
	k.bgs = map[string]*turn{"bg_live": {branch: "bg_live", task: "LIVE_AFTER_FOLD"}}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "go", time.Unix(9, 0))); err != nil {
		t.Fatal(err)
	}
	folded := 0
	for _, req := range p.reqs {
		hasFold, hasSnapshot := false, false
		for _, m := range req.Messages {
			if strings.HasPrefix(m.Content, "<turn_progress") {
				hasFold = true
			}
			if publishedSnapshot(m.Content) && strings.Contains(m.Content, "PERSON_IN_FOLD_49") && strings.Contains(m.Content, "LIVE_AFTER_FOLD") {
				hasSnapshot = true
			}
		}
		if hasFold {
			folded++
			if !hasSnapshot {
				t.Fatal("inline fold sent without live full snapshot")
			}
		}
	}
	if folded == 0 {
		t.Fatal("test did not fold")
	}
}
func TestEnvironmentLargeMemoryChangeRemainsReplayable(t *testing.T) {
	k, p := envBaselineKernel(t)
	old := strings.Repeat("O", 65)
	next := strings.Repeat("N", 65)
	path := filepath.Join(bot.Dir(k.Bot.Root, k.Bot.Name()), "memory", "memory.md")
	var before, after strings.Builder
	for i := 0; i < 30; i++ {
		before.WriteString("- (2026-10-22) " + old + strings.Repeat("x", i) + "\n")
		after.WriteString("- (2026-10-22) " + next + strings.Repeat("x", i) + "\n")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(before.String()), 0600); err != nil {
		t.Fatal(err)
	}
	// Establish the first set in the stream, keeping the prefix fixed.
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "see old", time.Unix(1000, 0))); err != nil {
		t.Fatal(err)
	}
	p.resps = append(p.resps, provider.Message{Role: provider.RoleAssistant, Content: "three"})
	oldText, err := k.memoryText(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(after.String()), 0600); err != nil {
		t.Fatal(err)
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "see new", time.Unix(1000, 0))); err != nil {
		t.Fatal(err)
	}
	curText, err := k.memoryText(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	add, remove := multisetDiff(factLines(oldText), factLines(curText))
	last := lastEnvUpdate(t, k)
	var message struct {
		Memory struct{ Add, Remove []string } `json:"memory"`
	}
	body := strings.TrimSuffix(strings.TrimPrefix(last, "<environment_update>\n"), "\n</environment_update>")
	if json.Unmarshal([]byte(body), &message) != nil || len(message.Memory.Add) != len(add) || len(message.Memory.Remove) != len(remove) || len(last) < 2048 {
		t.Fatalf("lossy large update: %s", last)
	}
}

func TestEnvironmentUnchangedWakeAppendsNoStateAndDropsLedger(t *testing.T) {
	k, _ := envBaselineKernel(t)
	before, _ := k.Bot.Count()
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "unchanged", time.Unix(1000, 0))); err != nil {
		t.Fatal(err)
	}
	after, _ := k.Bot.Count()
	if after-before != 2 {
		t.Fatalf("unchanged wake appended %d rows, want only input/reply", after-before)
	}
	raw, err := os.ReadFile(filepath.Join(bot.Dir(k.Bot.Root, k.Bot.Name()), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"env", "mem_seen", "mem_own"} {
		if _, ok := fields[key]; ok {
			t.Fatalf("saved shadow ledger %s", key)
		}
	}
}

func TestEnvironmentRestartDoesNotRepeatRunningOrQueuedTasks(t *testing.T) {
	hold := make(chan struct{})
	k, b, _ := dispatchKernel(t, &roleModel{hold: hold})
	defer finishAdmissionJobs(t, k, hold)
	for i := 0; i < 4; i++ {
		if _, err := k.dispatch(context.Background(), "research", "restart task "+string(rune('A'+i))); err != nil {
			t.Fatal(err)
		}
	}
	tasks, err := k.ensureExec().Tasks()
	if err != nil || len(tasks) != 4 {
		t.Fatalf("tasks: %v %v", tasks, err)
	}
	k.Memory = memory.Open(b.Root)
	if err := k.Memory.Remember(context.Background(), memory.Fact{Scope: memory.ScopePerson, PersonID: "u1", Tier: memory.TierProfile, Key: "home", Text: "RESTART_PERSON_38", WrittenBy: "u1"}); err != nil {
		t.Fatal(err)
	}
	k.Now = func() time.Time { return time.Unix(1000, 0) }
	k.Provider = &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "status ready"}}}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "status", time.Unix(1000, 0))); err != nil {
		t.Fatal(err)
	}
	rewriteEnvironmentState(t, k, nil)
	reloaded, err := bot.Load(b.Root, b.Name())
	if err != nil {
		t.Fatal(err)
	}
	restarted := &Kernel{Bot: reloaded, Memory: k.Memory, Exec: k.execSnapshot(), Now: k.Now}
	before, _ := b.Count()
	if _, err := restarted.receive(link.TerminalEvent("u1", "still here", time.Unix(1000, 0))); err != nil {
		t.Fatal(err)
	}
	st, err := b.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.syncEnv(st, link.Event{}, ""); err != nil {
		t.Fatal(err)
	}
	end, err := b.Count()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := b.Slice(before+1, end)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("restart test inspected no rows")
	}
	var updates strings.Builder
	for _, row := range rows {
		if strings.HasPrefix(row.Message.Content, "<environment_update>") {
			updates.WriteString(row.Message.Content)
		}
	}
	for _, task := range tasks {
		if strings.Contains(updates.String(), task.ID) {
			t.Fatalf("restart re-announced task: %s", updates.String())
		}
	}
	if strings.Contains(updates.String(), "RESTART_PERSON_38") {
		t.Fatal("restart repeated person")
	}
}
