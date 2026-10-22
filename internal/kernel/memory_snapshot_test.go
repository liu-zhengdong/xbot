package kernel

import (
	"context"
	"encoding/json"
	"fmt"
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

// TestMemorySnapshotPrefixStable freezes the memory message: a fact written by
// another process after the first wake must not change msgs[0] or msgs[1].
func TestMemorySnapshotPrefixStable(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	store := memory.Open(b.Root)
	t0 := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: "one"},
		{Role: provider.RoleAssistant, Content: "two"},
	}}
	if err := store.Remember(context.Background(), memory.Fact{
		Key: "early", Scope: memory.ScopeBot, Tier: memory.TierProfile,
		BotName: b.Name(), WrittenBy: b.Name(), Text: "seen before the snapshot", Time: t0,
	}); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{
		Bot: b, Provider: p, Memory: store, MaxSteps: 2,
		Now: func() time.Time { return t0 },
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "hello", t0)); err != nil {
		t.Fatal(err)
	}
	if err := store.Remember(context.Background(), memory.Fact{
		Key: "late", Scope: memory.ScopeBot, Tier: memory.TierProfile,
		BotName: b.Name(), WrittenBy: b.Name(), Text: "written after the snapshot", Time: t0,
	}); err != nil {
		t.Fatal(err)
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "again", t0.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) != 2 {
		t.Fatalf("requests %d", len(p.reqs))
	}
	if len(p.reqs[0].Messages) < 2 || len(p.reqs[1].Messages) < 2 {
		t.Fatalf("messages %d then %d", len(p.reqs[0].Messages), len(p.reqs[1].Messages))
	}
	for i := 0; i < 2; i++ {
		if p.reqs[0].Messages[i].Content != p.reqs[1].Messages[i].Content {
			t.Fatalf("msgs[%d] changed within the session\n%s\n---\n%s", i,
				p.reqs[0].Messages[i].Content, p.reqs[1].Messages[i].Content)
		}
	}
	if strings.Contains(p.reqs[1].Messages[1].Content, "written after the snapshot") {
		t.Fatal("a later write entered the frozen snapshot")
	}
}

// TestMemorySnapshotRefreshesOnCompaction checks that a compaction re-snapshots
// standing memory, so the next request sees a fact written before it.
func TestMemorySnapshotRefreshesOnCompaction(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	store := memory.Open(b.Root)
	t0 := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: "one"},
		{Role: provider.RoleAssistant, Content: "two"},
	}}
	k := &Kernel{
		Bot: b, Provider: p, Memory: store,
		Compactor: &recCompactor{}, Budget: compact.Budget{KeepTail: 1, Ceiling: 100000},
		MaxSteps: 2, Now: func() time.Time { return t0 },
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "first", t0)); err != nil {
		t.Fatal(err)
	}
	if err := store.Remember(context.Background(), memory.Fact{
		Key: "marker", Scope: memory.ScopeBot, Tier: memory.TierProfile,
		BotName: b.Name(), WrittenBy: b.Name(), Text: "fact before compaction", Time: t0,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := k.CompactRange(context.Background(), 1, 1, "compact it"); err != nil {
		t.Fatal(err)
	}
	st, err := b.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if !st.MemSnapSet || !strings.Contains(st.MemSnap, "fact before compaction") {
		t.Fatalf("snapshot not refreshed after compaction: %q", st.MemSnap)
	}
	if st.MemSeen != st.MemSnap {
		t.Fatalf("MemSeen %q != MemSnap %q", st.MemSeen, st.MemSnap)
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "second", t0.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) != 2 || len(p.reqs[1].Messages) < 2 {
		t.Fatalf("requests %d", len(p.reqs))
	}
	if !strings.Contains(p.reqs[1].Messages[1].Content, "fact before compaction") {
		t.Fatalf("next request used a stale snapshot\n%s", p.reqs[1].Messages[1].Content)
	}
}

// TestMemorySnapshotRecentLog checks the snapshot shows the newest 8 bot log
// facts, newest first, and never a person-scope log fact.
func TestMemorySnapshotRecentLog(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	store := memory.Open(b.Root)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		if err := store.Remember(ctx, memory.Fact{
			Key: fmt.Sprintf("log-%02d", i), Scope: memory.ScopeBot, Tier: memory.TierLog,
			BotName: b.Name(), Text: fmt.Sprintf("bot-log-%02d", i), Time: t0.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Remember(ctx, memory.Fact{
		Key: "person-log", Scope: memory.ScopePerson, Tier: memory.TierLog,
		PersonID: "u1", WrittenBy: "u1", Text: "person-log-secret", Time: t0.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, Memory: store}
	body, err := k.memoryText(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "### Recent log (8 of 10; older: recall)") {
		t.Fatalf("missing recent-log heading:\n%s", body)
	}
	if strings.Contains(body, "person-log-secret") {
		t.Fatalf("person log leaked into the snapshot:\n%s", body)
	}
	last := -1
	for i := 9; i >= 2; i-- {
		text := fmt.Sprintf("- bot-log-%02d", i)
		at := strings.Index(body, text)
		if at < 0 {
			t.Fatalf("missing newest log line %q:\n%s", text, body)
		}
		if at < last {
			t.Fatalf("log lines are not newest first:\n%s", body)
		}
		last = at
	}
	for _, old := range []string{"bot-log-00", "bot-log-01"} {
		if strings.Contains(body, old) {
			t.Fatalf("old log line %q shown:\n%s", old, body)
		}
	}
}

// memDiffRun runs wakes on one bot, one minute apart, and
// calls between(i) after wake i. It returns the requests in order.
func memDiffRun(t *testing.T, k *Kernel, wakes int, between func(int)) []provider.Request {
	t.Helper()
	t0 := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	var reqs []provider.Request
	for i := 0; i < wakes; i++ {
		if err := k.Handle(context.Background(), link.TerminalEvent("u1", fmt.Sprintf("wake %d", i), t0.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatal(err)
		}
		if between != nil {
			between(i)
		}
	}
	if p, ok := k.Provider.(*scripted); ok {
		reqs = p.reqs
	}
	return reqs
}

func lastEnvUpdate(t *testing.T, k *Kernel) string {
	t.Helper()
	ups := envUpdates(t, k.Bot)
	if len(ups) == 0 {
		return ""
	}
	return ups[len(ups)-1]
}

// TestMemoryDiffAppendedForOutsideWrite: an outside write reaches the bot as a
// diff in the environment update while msgs[1] stays byte-identical.
func TestMemoryDiffAppendedForOutsideWrite(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	store := memory.Open(b.Root)
	t0 := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: "one"},
		{Role: provider.RoleAssistant, Content: "two"},
	}}
	k := &Kernel{Bot: b, Provider: p, Memory: store, MaxSteps: 2, Now: func() time.Time { return t0 }}
	reqs := memDiffRun(t, k, 2, func(i int) {
		if i != 0 {
			return
		}
		if err := store.Remember(context.Background(), memory.Fact{
			Key: "outside", Scope: memory.ScopeBot, Tier: memory.TierProfile,
			BotName: b.Name(), WrittenBy: "owner", Text: "deploys freeze on fridays", Time: t0,
		}); err != nil {
			t.Fatal(err)
		}
	})
	if len(reqs) != 2 || len(reqs[1].Messages) < 2 {
		t.Fatalf("requests %d", len(reqs))
	}
	if reqs[0].Messages[1].Content != reqs[1].Messages[1].Content {
		t.Fatal("msgs[1] changed after an outside memory write")
	}
	up := lastEnvUpdate(t, k)
	if !strings.Contains(up, "memory changed since you last saw it") || !strings.Contains(up, "+ ") || !strings.Contains(up, "deploys freeze on fridays") {
		t.Fatalf("no memory diff in the update:\n%s", up)
	}
	if strings.Contains(up, "memory: updated") {
		t.Fatalf("old hash line still emitted:\n%s", up)
	}
	var all strings.Builder
	for _, m := range reqs[1].Messages {
		all.WriteString(m.Content)
	}
	if n := strings.Count(all.String(), "deploys freeze on fridays"); n != 1 {
		t.Fatalf("fact appears %d times in the request, want once (the diff)", n)
	}
}

// TestMemoryDiffSkipsOwnWrite: the bot's own file write is not reported back.
func TestMemoryDiffSkipsOwnWrite(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameMemory, link.NameTools)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	store := memory.Open(b.Root)
	t0 := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{memoryWriteCall(b, "c1", memory.ScopeBot, "owner prefers short replies")}},
		{Role: provider.RoleAssistant, Content: "noted"},
		{Role: provider.RoleAssistant, Content: "two"},
	}}
	k := &Kernel{
		Bot: b, Provider: p, Memory: store, MaxSteps: 4,
		Links: []link.Link{link.Terminal{}, link.Memory{}, link.NewToolsLinkAt(b.Root, "")},
		Now:   func() time.Time { return t0 },
	}
	reqs := memDiffRun(t, k, 2, nil)
	if len(reqs) != 3 {
		t.Fatalf("requests %d", len(reqs))
	}
	assertMemoryFileWrites(t, b, 1)
	facts, err := store.Recall(context.Background(), memory.Query{Scope: memory.ScopeBot, BotName: b.Name()})
	if err != nil || len(facts) != 1 {
		t.Fatalf("facts %v %v", facts, err)
	}
	for _, up := range envUpdates(t, b) {
		if strings.Contains(up, "owner prefers short replies") || strings.Contains(up, "memory: updated") {
			t.Fatalf("own write reported back:\n%s", up)
		}
	}
	if reqs[0].Messages[1].Content != reqs[2].Messages[1].Content {
		t.Fatal("msgs[1] changed after the bot's own file edit")
	}
}

// TestMemoryDiffShowsPersonWriteByOtherBot: another bot's person-scope write shows.
func TestMemoryDiffShowsPersonWriteByOtherBot(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	store := memory.Open(b.Root)
	t0 := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: "one"},
		{Role: provider.RoleAssistant, Content: "two"},
	}}
	k := &Kernel{Bot: b, Provider: p, Memory: store, MaxSteps: 2, Now: func() time.Time { return t0 }}
	memDiffRun(t, k, 2, func(i int) {
		if i != 0 {
			return
		}
		if err := store.Remember(context.Background(), memory.Fact{
			Key: "tz", Scope: memory.ScopePerson, Tier: memory.TierProfile,
			PersonID: "u1", WrittenBy: "otherbot", Text: "timezone Asia/Shanghai", Time: t0,
		}); err != nil {
			t.Fatal(err)
		}
	})
	if up := lastEnvUpdate(t, k); !strings.Contains(up, "timezone Asia/Shanghai") {
		t.Fatalf("other bot's person write not shown:\n%s", up)
	}
}

// TestMemoryDiffCap: a long diff collapses to a count; equal texts give nothing.
func TestMemoryDiffCap(t *testing.T) {
	var a, b strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&a, "- old fact number %03d with some padding text\n", i)
		fmt.Fprintf(&b, "- new fact number %03d with some padding text\n", i)
	}
	got := memoryDiff(a.String(), b.String(), nil)
	if got != "memory changed: 100 lines added, 100 removed (too long to show; use recall to read it)" {
		t.Fatalf("got %q", got)
	}
	if memoryDiff(a.String(), a.String(), nil) != "" {
		t.Fatal("equal texts produced a diff")
	}
	if memoryDiff("x\n", "x\n\n\n", nil) != "" {
		t.Fatal("blank-line change produced a diff")
	}
	if d := memoryDiff("a\nb\n", "a\n<b>\n", nil); !strings.Contains(d, "+ &lt;b&gt;") || !strings.Contains(d, "- b") {
		t.Fatalf("diff not fenced or wrong: %q", d)
	}
}

// TestMemoryDiffQuietWithoutChange: no change, no memory text in the update.
func TestMemoryDiffQuietWithoutChange(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	store := memory.Open(b.Root)
	t0 := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	if err := store.Remember(context.Background(), memory.Fact{
		Key: "k", Scope: memory.ScopeBot, Tier: memory.TierProfile,
		BotName: b.Name(), WrittenBy: b.Name(), Text: "stable fact", Time: t0,
	}); err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: "one"},
		{Role: provider.RoleAssistant, Content: "two"},
	}}
	k := &Kernel{Bot: b, Provider: p, Memory: store, MaxSteps: 2, Now: func() time.Time { return t0 }}
	reqs := memDiffRun(t, k, 2, nil)
	if len(reqs) != 2 || reqs[0].Messages[1].Content != reqs[1].Messages[1].Content {
		t.Fatal("msgs[1] changed without a memory change")
	}
	ups := envUpdates(t, b)
	for _, up := range ups[1:] {
		if strings.Contains(up, "memory changed") || strings.Contains(up, "memory: updated") {
			t.Fatalf("memory reported without a change:\n%s", up)
		}
	}
}

// TestMemoryViewPerPerson: a turn from another person never sees the owner's
// person facts, in msgs[1] or in a diff, and the owner's view comes back intact.
func TestMemoryViewPerPerson(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	store := memory.Open(b.Root)
	t0 := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	if err := store.Remember(context.Background(), memory.Fact{
		Key: "home", Scope: memory.ScopePerson, Tier: memory.TierProfile,
		PersonID: "u1", WrittenBy: "u1", Text: "owner lives in Hangzhou", Time: t0,
	}); err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: "one"},
		{Role: provider.RoleAssistant, Content: "two"},
		{Role: provider.RoleAssistant, Content: "three"},
	}}
	k := &Kernel{Bot: b, Provider: p, Memory: store, MaxSteps: 2, Now: func() time.Time { return t0 }}
	for i, who := range []string{"u1", "u2", "u1"} {
		if err := k.Handle(context.Background(), link.TerminalEvent(who, fmt.Sprintf("hi %d", i), t0.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatal(err)
		}
	}
	if len(p.reqs) != 3 {
		t.Fatalf("requests %d", len(p.reqs))
	}
	for _, req := range p.reqs {
		if strings.Contains(req.Messages[1].Content, "owner lives in Hangzhou") {
			t.Fatal("person fact in fixed prefix")
		}
	}
	if !strings.Contains(messagesText(p.reqs[0].Messages), "owner lives in Hangzhou") {
		t.Fatal("missing person environment")
	}
	if p.reqs[0].Messages[1].Content != p.reqs[2].Messages[1].Content {
		t.Fatal("owner's snapshot not restored")
	}
	for _, up := range envUpdates(t, b) {
		if strings.Contains(up, "memory changed") {
			t.Fatalf("person switch reported as a memory change:\n%s", up)
		}
	}
}

// hookProvider runs hook before the first model call.
type hookProvider struct {
	*scripted
	hook func()
}

func (h *hookProvider) Chat(ctx context.Context, req provider.Request, onDelta func(string)) (provider.Message, error) {
	if h.hook != nil {
		h.hook()
		h.hook = nil
	}
	return h.scripted.Chat(ctx, req, onDelta)
}

// TestMemoryDiffKeepsOutsideEditDuringOwnWrite: an outside edit landing in the
// same turn as the bot's own file edit is still reported; only the own line is skipped.
func TestMemoryDiffKeepsOutsideEditDuringOwnWrite(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameMemory, link.NameTools)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	store := memory.Open(b.Root)
	t0 := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	path := filepath.Join(b.Root, "bots", b.Name(), "memory", "memory.md")
	readArgs, _ := json.Marshal(map[string]string{"path": path})
	editArgs, _ := json.Marshal(map[string]string{"path": path, "old_string": "outside edit mid turn", "new_string": "outside edit mid turn\n- (2026-10-17) own note from the bot"})
	sp := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "read", Type: "function", Function: provider.FunctionCall{Name: "read_file", Arguments: string(readArgs)}}}},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "edit", Type: "function", Function: provider.FunctionCall{Name: "edit_file", Arguments: string(editArgs)}}}},
		{Role: provider.RoleAssistant, Content: "noted"},
		{Role: provider.RoleAssistant, Content: "two"},
	}}
	hp := &hookProvider{scripted: sp, hook: func() {
		if err := store.Remember(context.Background(), memory.Fact{
			Key: "outside", Scope: memory.ScopeBot, Tier: memory.TierProfile,
			BotName: b.Name(), WrittenBy: "owner", Text: "outside edit mid turn", Time: t0,
		}); err != nil {
			t.Error(err)
		}
	}}
	k := &Kernel{
		Bot: b, Provider: hp, Memory: store, MaxSteps: 4,
		Links: []link.Link{link.Terminal{}, link.Memory{}, link.NewToolsLinkAt(b.Root, "")},
		Now:   func() time.Time { return t0 },
	}
	memDiffRun(t, k, 2, nil)
	assertMemoryFileWrites(t, b, 1)
	up := lastEnvUpdate(t, k)
	if !strings.Contains(up, "outside edit mid turn") {
		t.Fatalf("outside edit swallowed by the own write:\n%s", up)
	}
	if strings.Contains(up, "own note from the bot") {
		t.Fatalf("own write reported back:\n%s", up)
	}
}

// TestMemoryDiffRecentLogOnlyNewEntries: a log append shows the new entry only,
// not the heading count or the entry that left the window.
func TestMemoryDiffRecentLogOnlyNewEntries(t *testing.T) {
	render := func(from, to, total int) string {
		var b strings.Builder
		b.WriteString("### Standing\n- a fact\n")
		fmt.Fprintf(&b, "\n%s (%d of %d; older: recall)\n", recentLogHeading, to-from, total)
		for i := to - 1; i >= from; i-- {
			fmt.Fprintf(&b, "- log entry %02d\n", i)
		}
		return b.String()
	}
	got := memoryDiff(render(2, 10, 10), render(3, 11, 11), nil)
	if !strings.Contains(got, "+ [Recent log] - log entry 10") {
		t.Fatalf("new entry missing: %q", got)
	}
	if strings.Contains(got, "log entry 02") || strings.Contains(got, "of 11") {
		t.Fatalf("window shift or heading reported: %q", got)
	}
}

// TestMemoryOwnWriteInForkedTurnSkipped: a file write made after the turn forked
// is still the bot's own change; the next wake does not report it back.
func TestMemoryOwnWriteInForkedTurnSkipped(t *testing.T) {
	k, g, box := forkKernel(t)
	store := memory.Open(k.Bot.Root)
	k.Memory = store
	k.Links = append(k.Links, link.Memory{}, link.NewToolsLinkAt(k.Bot.Root, ""))
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "gate", Arguments: "{}"}}}},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{memoryWriteCall(k.Bot, "c2", memory.ScopeBot, "written in the background")}},
		{Role: provider.RoleAssistant, Content: "后台做完了"},
		{Role: provider.RoleAssistant, Content: "next"},
	}}
	k.Provider = p
	ev := link.TerminalEvent("u1", "做个长任务", time.Unix(10, 0).UTC())
	if err := box.Post(ev); err != nil {
		t.Fatal(err)
	}
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	close(g.release)
	waitNoBackgrounds(t, k)
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "and now", time.Unix(30, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	assertMemoryFileWrites(t, k.Bot, 1)
	facts, err := store.Recall(context.Background(), memory.Query{Scope: memory.ScopeBot, BotName: k.Bot.Name()})
	if err != nil || len(facts) != 1 {
		t.Fatalf("facts %v %v", facts, err)
	}
	for _, up := range envUpdates(t, k.Bot) {
		if strings.Contains(up, "written in the background") {
			t.Fatalf("background own write reported back:\n%s", up)
		}
	}
}

// TestMemoryTimerWakeKeepsView: a timer wake keeps the owner's view, so msgs[1]
// does not flip between the owner's turn and the bot's own follow-up wake.
func TestMemoryTimerWakeKeepsView(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	store := memory.Open(b.Root)
	t0 := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	if err := store.Remember(context.Background(), memory.Fact{
		Key: "home", Scope: memory.ScopePerson, Tier: memory.TierProfile,
		PersonID: "u1", WrittenBy: "u1", Text: "owner lives in Hangzhou", Time: t0,
	}); err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: "one"},
		{Role: provider.RoleAssistant, Content: "two"},
	}}
	k := &Kernel{Bot: b, Provider: p, Memory: store, MaxSteps: 2, Now: func() time.Time { return t0 }}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "hi", t0)); err != nil {
		t.Fatal(err)
	}
	if err := k.Handle(context.Background(), link.NewEvent(link.NameTimer, link.TypeTimer, "tick", "timer", t0.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) != 2 || p.reqs[0].Messages[1].Content != p.reqs[1].Messages[1].Content {
		t.Fatal("timer wake changed msgs[1]")
	}
}

// TestMemoryOwnWriteAcrossViews: the owner's bot-scope write is not reported to
// the shared view, and the owner's person-scope write never reaches it at all.
func TestMemoryOwnWriteAcrossViews(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameMemory, link.NameTools)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	store := memory.Open(b.Root)
	t0 := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: "hello u2"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
			memoryWriteCall(b, "c1", memory.ScopeBot, "team uses go 1.24"),
			memoryWriteCall(b, "c2", memory.ScopePerson, "owner birthday in may"),
		}},
		{Role: provider.RoleAssistant, Content: "noted"},
		{Role: provider.RoleAssistant, Content: "owner again"},
		{Role: provider.RoleAssistant, Content: "hello again"},
	}}
	k := &Kernel{
		Bot: b, Provider: p, Memory: store, MaxSteps: 4,
		Links: []link.Link{link.Terminal{}, link.Memory{}, link.NewToolsLinkAt(b.Root, "")},
		Now:   func() time.Time { return t0 },
	}
	// u1 speaks twice, so the own writes are drained into the parked u2 view.
	for i, who := range []string{"u2", "u1", "u1", "u2"} {
		if err := k.Handle(context.Background(), link.TerminalEvent(who, fmt.Sprintf("hi %d", i), t0.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatal(err)
		}
	}
	assertMemoryFileWrites(t, b, 2)
	if len(p.reqs) != 5 {
		t.Fatalf("requests %d", len(p.reqs))
	}
	for _, up := range envUpdates(t, b) {
		if strings.Contains(up, "team uses go 1.24") || strings.Contains(up, "owner birthday in may") {
			t.Fatalf("own write reported to another view:\n%s", up)
		}
	}
	if strings.Contains(p.reqs[4].Messages[1].Content, "owner birthday") {
		t.Fatal("owner person fact in the shared view")
	}
}

// TestMemoryDiffReportsOutsideRemovalOfOwnLine: another writer removing a line
// the bot itself added is still reported.
func TestMemoryDiffReportsOutsideRemovalOfOwnLine(t *testing.T) {
	old := "### Standing\n- a fact\n"
	own := []string{"+[Standing] - bot added this"}
	got := memoryDiff(old, old, own)
	if !strings.Contains(got, "- [Standing] - bot added this") {
		t.Fatalf("outside removal of an own line hidden: %q", got)
	}
	if memoryDiff(old, old+"- bot added this\n", own) != "" {
		t.Fatal("own line reported back")
	}
}

// A brief worker uses the same frozen Bot snapshot regardless of sender.
func TestMemSnapForPicksTurnView(t *testing.T) {
	st := bot.State{MemSnapSet: true, MemSnap: "bot snap"}
	k := &Kernel{}
	got, err := k.memSnapFor(context.Background(), st, "")
	if err != nil || got != "bot snap" {
		t.Fatalf("got %q %v", got, err)
	}
}

// Compaction refreshes Bot memory without putting person facts into the prefix.
func TestRefreshMemSnapKeepsActiveKey(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	store := memory.Open(b.Root)
	if err := store.Remember(context.Background(), memory.Fact{
		Key: "home", Scope: memory.ScopePerson, Tier: memory.TierProfile,
		PersonID: "u1", WrittenBy: "u1", Text: "owner lives in Hangzhou", Time: time.Unix(1, 0).UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, Memory: store, turn: &turn{turnSender: "u2", turnKind: link.KindPerson}}
	st := bot.State{MemSnapSet: true, MemSnap: "old", MemSeen: "old"}
	k.refreshMemSnap(context.Background(), &st)
	if strings.Contains(st.MemSnap, "owner lives in Hangzhou") {
		t.Fatalf("refreshed for the wrong view: %q %q", "bot-only", st.MemSnap)
	}
}

// TestMemorySubagentWakeKeepsView: a subagent or fork_end wake after a guest's
// turn keeps the guest's view; the owner's person facts do not appear.
func TestMemorySubagentWakeKeepsView(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	store := memory.Open(b.Root)
	t0 := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	if err := store.Remember(context.Background(), memory.Fact{
		Key: "home", Scope: memory.ScopePerson, Tier: memory.TierProfile,
		PersonID: "u1", WrittenBy: "u1", Text: "owner lives in Hangzhou", Time: t0,
	}); err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: "one"},
		{Role: provider.RoleAssistant, Content: "two"},
	}}
	k := &Kernel{Bot: b, Provider: p, Memory: store, MaxSteps: 2, Now: func() time.Time { return t0 }}
	if err := k.Handle(context.Background(), link.TerminalEvent("u2", "hi", t0)); err != nil {
		t.Fatal(err)
	}
	if err := k.Handle(context.Background(), link.NewEvent(link.SourceSubagent, "fork_end", "done", "bg_1", t0.Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) != 2 || p.reqs[0].Messages[1].Content != p.reqs[1].Messages[1].Content {
		t.Fatal("subagent wake changed msgs[1]")
	}
	if strings.Contains(p.reqs[1].Messages[1].Content, "owner lives in Hangzhou") {
		t.Fatal("owner fact in a guest's background wake")
	}
}

// TestMemoryDiffReportsUndoneOwnWrite: the bot remembers X, someone removes X
// before the next wake; live equals what was seen, but the removal is reported.
func TestMemoryDiffReportsUndoneOwnWrite(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameMemory, link.NameTools)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	store := memory.Open(b.Root)
	t0 := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{memoryWriteCall(b, "c1", memory.ScopeBot, "short lived note")}},
		{Role: provider.RoleAssistant, Content: "noted"},
		{Role: provider.RoleAssistant, Content: "two"},
	}}
	k := &Kernel{
		Bot: b, Provider: p, Memory: store, MaxSteps: 4,
		Links: []link.Link{link.Terminal{}, link.Memory{}, link.NewToolsLinkAt(b.Root, "")},
		Now:   func() time.Time { return t0 },
	}
	memDiffRun(t, k, 2, func(i int) {
		if i != 0 {
			return
		}
		q := memory.Query{Scope: memory.ScopeBot, BotName: b.Name()}
		if err := store.Forget(context.Background(), q, "short lived note"); err != nil {
			t.Fatal(err)
		}
	})
	assertMemoryFileWrites(t, b, 1)
	if up := lastEnvUpdate(t, k); !strings.Contains(up, "- [") || !strings.Contains(up, "short lived note") {
		t.Fatalf("undone own write not reported:\n%s", up)
	}
}

// TestRefreshMemSnapClearsOwnQueue: a refreshed snapshot already holds pending
// own writes, so they must not be applied again.
func TestRefreshMemSnapClearsOwnQueue(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, Memory: memory.Open(b.Root)}
	k.memOwn = []ownChange{{lines: []string{"+[Bot] - x"}}}
	st := bot.State{MemSnapSet: true}
	k.refreshMemSnap(context.Background(), &st)
	if len(k.memOwn) != 0 {
		t.Fatalf("own queue survived the refresh: %+v", k.memOwn)
	}
}

// TestMemoryViewCapKeepsTarget: switching to a parked view at the cap keeps it.
func TestMemoryAcceptedProfilesDoNotClip(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	store := memory.Open(b.Root)
	ctx := context.Background()
	for _, scope := range []memory.Scope{memory.ScopeBot, memory.ScopePerson} {
		for i := 0; ; i++ {
			f := memory.Fact{Scope: scope, BotName: b.Name(), PersonID: "u1", Tier: memory.TierProfile, WrittenBy: "dev", Text: fmt.Sprintf("%s fact %03d abcdefghijklmnop", scope, i)}
			if i%10 != 0 {
				f.Key = fmt.Sprintf("%s-custom-%03d-%s", scope, i, strings.Repeat("k", 70))
			}
			if err := store.Remember(ctx, f); err != nil {
				if i < 4 {
					t.Fatal(err)
				}
				break
			}
		}
	}
	k := &Kernel{Bot: b, Memory: store}
	body, err := k.memoryText(ctx, "u1")
	if err != nil || strings.Contains(body, memoryClipNote) {
		t.Fatalf("accepted profiles clipped: %v\n%s", err, body)
	}
}

func TestMemoryRenderUpgradeResnapshots(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	store := memory.Open(b.Root)
	ctx := context.Background()
	if err := store.Remember(ctx, memory.Fact{Scope: memory.ScopeBot, BotName: b.Name(), Tier: memory.TierProfile, Key: "rule", Text: "keep it small", WrittenBy: "dev"}); err != nil {
		t.Fatal(err)
	}
	old := "## Bot\n- rule: keep it small (written by dev)\n"
	if err := b.SaveState(bot.State{MemSnapSet: true, MemSnap: old, MemSeen: old, MemOwn: []string{"+old"}}); err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "ok"}}}
	k := &Kernel{Bot: b, Memory: store, Provider: p, MaxSteps: 2}
	fresh, err := k.memoryText(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := k.Handle(ctx, link.TerminalEvent("u1", "hello", time.Now())); err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) != 1 || len(p.reqs[0].Messages) < 2 {
		t.Fatal("missing request messages")
	}
	msgs := p.reqs[0].Messages
	if msgs[1].Content != wrapMemory(fresh) {
		t.Fatalf("snapshot = %q, want %q", msgs[1].Content, fresh)
	}
	for _, msg := range msgs {
		if strings.Contains(msg.Content, "<memory_diff>") || strings.Contains(msg.Content, "memory changed") {
			t.Fatalf("upgrade emitted bogus diff: %s", msg.Content)
		}
	}
	st, err := b.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if st.MemRender != memRenderVersion || len(st.MemOwn) != 0 {
		t.Fatalf("old views survived: %+v", st)
	}
}

// memoryWriteCall emits a real file-tool call; the kernel owns bookkeeping.
func memoryWriteCall(b *bot.Bot, id string, scope memory.Scope, text string) provider.ToolCall {
	path := filepath.Join(b.Root, "bots", b.Name(), "memory", "memory.md")
	if scope == memory.ScopePerson {
		path = filepath.Join(b.Root, "people", "u1", "memory", "memory.md")
		text += " via " + b.Name()
	}
	raw, _ := json.Marshal(map[string]string{"path": path, "content": "- (2026-10-17) " + text + "\n"})
	return provider.ToolCall{ID: id, Type: "function", Function: provider.FunctionCall{Name: "write_file", Arguments: string(raw)}}
}

func assertMemoryFileWrites(t *testing.T, b *bot.Bot, want int) {
	t.Helper()
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	wrote := 0
	for _, entry := range entries {
		m := entry.Message
		if m.Role != provider.RoleTool || !isFileEdit(m.Name) {
			continue
		}
		if !strings.HasPrefix(m.Content, "wrote ") && !strings.HasPrefix(m.Content, "updated ") {
			t.Fatalf("file write failed: %s", m.Content)
		}
		wrote++
	}
	if wrote != want {
		t.Fatalf("real file writes = %d, want %d", wrote, want)
	}
}
