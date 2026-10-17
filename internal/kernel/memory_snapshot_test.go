package kernel

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

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
		Compactor: &recCompactor{}, Budget: compact.Budget{KeepTail: 1, CompactAt: 100000},
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

// TestMemoryDiffSkipsOwnRemember: the bot's own remember is not reported back.
func TestMemoryDiffSkipsOwnRemember(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameMemory)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	store := memory.Open(b.Root)
	t0 := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	args := `{"fact":"owner prefers short replies","tier":"profile"}`
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "remember", Arguments: args},
		}}},
		{Role: provider.RoleAssistant, Content: "noted"},
		{Role: provider.RoleAssistant, Content: "two"},
	}}
	k := &Kernel{
		Bot: b, Provider: p, Memory: store, MaxSteps: 4,
		Links: []link.Link{link.Terminal{}, link.Memory{}},
		Now:   func() time.Time { return t0 },
	}
	reqs := memDiffRun(t, k, 2, nil)
	if len(reqs) != 3 {
		t.Fatalf("requests %d", len(reqs))
	}
	facts, err := store.Recall(context.Background(), memory.Query{Scope: memory.ScopeBot, BotName: b.Name()})
	if err != nil || len(facts) != 1 {
		t.Fatalf("facts %v %v", facts, err)
	}
	for _, up := range envUpdates(t, b) {
		if strings.Contains(up, "memory changed") || strings.Contains(up, "memory: updated") {
			t.Fatalf("own write reported back:\n%s", up)
		}
	}
	if reqs[0].Messages[1].Content != reqs[2].Messages[1].Content {
		t.Fatal("msgs[1] changed after the bot's own remember")
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
	for _, m := range p.reqs[1].Messages {
		if strings.Contains(m.Content, "owner lives in Hangzhou") {
			t.Fatalf("owner fact reached another person's turn:\n%s", m.Content)
		}
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
// same turn as the bot's own remember is still reported; only the own line is skipped.
func TestMemoryDiffKeepsOutsideEditDuringOwnWrite(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameMemory)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	store := memory.Open(b.Root)
	t0 := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)
	args := `{"fact":"own note from the bot","tier":"profile"}`
	sp := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "remember", Arguments: args},
		}}},
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
		Links: []link.Link{link.Terminal{}, link.Memory{}},
		Now:   func() time.Time { return t0 },
	}
	memDiffRun(t, k, 2, nil)
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
	if !strings.Contains(got, "+ - log entry 10") {
		t.Fatalf("new entry missing: %q", got)
	}
	if strings.Contains(got, "log entry 02") || strings.Contains(got, "Recent log") {
		t.Fatalf("window shift or heading reported: %q", got)
	}
}

// TestMemoryOwnWriteInForkedTurnSaved: a remember made after the turn forked is
// saved as the bot's own change when the background finishes.
func TestMemoryOwnWriteInForkedTurnSaved(t *testing.T) {
	k, g, box := forkKernel(t)
	store := memory.Open(k.Bot.Root)
	k.Memory = store
	k.Links = append(k.Links, link.Memory{})
	k.Provider = &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "gate", Arguments: "{}"}}}},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c2", Type: "function", Function: provider.FunctionCall{Name: "remember", Arguments: `{"fact":"written in the background","tier":"profile"}`}}}},
		{Role: provider.RoleAssistant, Content: "后台做完了"},
	}}
	ev := link.TerminalEvent("u1", "做个长任务", time.Unix(10, 0).UTC())
	if err := box.Post(ev); err != nil {
		t.Fatal(err)
	}
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	close(g.release)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(k.Backgrounds()) != 0 {
		time.Sleep(20 * time.Millisecond)
	}
	st, err := k.Bot.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range st.MemOwn {
		found = found || strings.Contains(l, "written in the background")
	}
	if !found {
		t.Fatalf("background own write not saved: %q", st.MemOwn)
	}
}
