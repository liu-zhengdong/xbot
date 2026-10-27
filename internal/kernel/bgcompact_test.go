package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/fsutil"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

func TestBackgroundCompactDoesNotBlockTurn(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	seedHistory(t, b, 8, 4000)
	mem := &factMem{}
	comp := &gateCompactor{release: make(chan struct{}), entered: make(chan struct{})}
	k := &Kernel{
		Bot: b, Provider: &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("ok")}}},
		Compactor: comp, Memory: mem, Extractor: stubExtract{facts: []memory.Fact{{
			Scope: memory.ScopeBot, Tier: memory.TierLog, Text: "kept from the old range",
		}}},
		MaxSteps: 2,
	}
	tuneSoftBudget(t, k)

	done := make(chan error, 1)
	start := time.Now()
	go func() {
		done <- k.Handle(context.Background(), link.TerminalEvent("u1", "LATE", time.Unix(30, 0).UTC()))
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("handle blocked on background compaction")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("handle took %s", time.Since(start))
	}
	waitCompactor(t, comp)
	job := readJobFile(t, b)
	if job.Status != jobRunning || job.To < 1 {
		t.Fatalf("job %+v", job)
	}
	st, err := b.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Blocks) != 0 {
		t.Fatalf("block swapped while the job is still running: %+v", st.Blocks)
	}
	if n := mem.count(); n != 0 {
		t.Fatalf("facts stored before the summary finished: %d", n)
	}

	k2 := &Kernel{
		Bot: b, Provider: &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("still")}}},
		Compactor: comp, Memory: mem, Extractor: k.Extractor, Budget: k.Budget, MaxSteps: 2,
	}
	done2 := make(chan error, 1)
	go func() {
		done2 <- k2.Handle(context.Background(), link.TerminalEvent("u1", "DURING", time.Unix(31, 0).UTC()))
	}()
	select {
	case err := <-done2:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("second handle blocked while compaction was running")
	}
	if calls := comp.ncalls(); calls != 1 {
		t.Fatalf("compactor calls %d, want 1", calls)
	}
	job2 := readJobFile(t, b)
	if job2.From != job.From || job2.To != job.To || job2.Status != jobRunning {
		t.Fatalf("job changed while running: %+v then %+v", job, job2)
	}

	close(comp.release)
	waitJob(t, b, jobReady)
	st, err = b.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Blocks) != 0 {
		t.Fatalf("ready job swapped outside a turn: %+v", st.Blocks)
	}
	if mem.count() != 0 {
		t.Fatalf("background compaction wrote %d facts to memory", mem.count())
	}
	for _, turn := range comp.snapshot() {
		if strings.Contains(turn.Text(), "LATE") || strings.Contains(turn.Text(), "DURING") {
			t.Fatalf("range included a message that arrived with the job: %s", turn.Text())
		}
	}

	k3 := &Kernel{
		Bot: b, Provider: &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("next")}}},
		Compactor: comp, Memory: mem, Extractor: k.Extractor, Budget: k.Budget, MaxSteps: 2,
	}
	if err := k3.Handle(context.Background(), link.TerminalEvent("u1", "AFTER", time.Unix(32, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	st, err = b.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	active := ActiveBlocks(st.Blocks)
	if len(active) != 1 || active[0].Text != "SUM" || active[0].Focus != "background" {
		t.Fatalf("blocks %+v", st.Blocks)
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range entries {
		if strings.Contains(e.Message.Text(), "DURING") || strings.Contains(e.Message.Text(), "AFTER") || strings.Contains(e.Message.Text(), "LATE") {
			if active[0].To >= i+1 {
				t.Fatalf("summary %d-%d covers later message %d %q", active[0].From, active[0].To, i+1, e.Message.Text())
			}
		}
	}
	if mem.count() != 0 {
		t.Fatalf("applying the summary wrote %d facts to memory", mem.count())
	}
	if _, err := os.Stat(filepath.Join(bot.Dir(b.Root, b.Name()), "compact-job.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("job file after apply: %v", err)
	}
}

func TestBackgroundSwapOnlyBetweenTurns(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	seedHistory(t, b, 8, 4000)
	comp := &noteCompactor{}
	chat := &holdChat{entered: make(chan struct{}), release: make(chan struct{})}
	k := &Kernel{
		Bot: b, Provider: chat, Compactor: comp, Memory: &factMem{},
		Extractor: stubExtract{facts: []memory.Fact{{Scope: memory.ScopeBot, Tier: memory.TierLog, Text: "from the job"}}},
		MaxSteps:  2,
	}
	tuneSoftBudget(t, k)
	done := make(chan error, 1)
	go func() {
		done <- k.Handle(context.Background(), link.TerminalEvent("u1", "ask", time.Unix(40, 0).UTC()))
	}()
	select {
	case <-chat.entered:
	case err := <-done:
		t.Fatalf("handle returned before the model call: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("model was not called")
	}
	waitJob(t, b, jobReady)
	st, err := b.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Blocks) != 0 {
		t.Fatalf("block appeared mid-turn: %+v", st.Blocks)
	}
	close(chat.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handle did not finish")
	}
	st, err = b.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(ActiveBlocks(st.Blocks)) != 1 {
		t.Fatalf("block not swapped after the turn: %+v", st.Blocks)
	}
}

func TestBackgroundJobCrashIsIdempotent(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	for i := 0; i < 5; i++ {
		if err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("old line")}}); err != nil {
			t.Fatal(err)
		}
	}
	job := compactJob{From: 1, To: 3, Focus: defaultFocus, Status: jobRunning, Summary: "PARTIAL"}
	raw, err := fsutil.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if err := fsutil.WriteAtomic(filepath.Join(bot.Dir(b.Root, b.Name()), "compact-job.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	mem := &factMem{}
	comp := &flakyCompactor{fail: 1}
	k := &Kernel{
		Bot: b, Provider: &scripted{resps: []provider.Message{
			{Role: provider.RoleAssistant, Content: provider.TextContent("one")},
			{Role: provider.RoleAssistant, Content: provider.TextContent("two")},
			{Role: provider.RoleAssistant, Content: provider.TextContent("three")},
		}},
		Compactor: comp, Memory: mem,
		Extractor: stubExtract{facts: []memory.Fact{{Scope: memory.ScopeBot, Tier: memory.TierLog, Text: "durable"}}},
		Budget:    compact.Budget{Ceiling: 1_000_000, KeepTail: 1}, MaxSteps: 2,
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "first", time.Unix(50, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	waitCalls(t, comp, 1)
	st, err := b.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Blocks) != 0 {
		t.Fatalf("partial block after a failed job: %+v", st.Blocks)
	}
	got := readJobFile(t, b)
	if got.Status != jobRunning {
		t.Fatalf("job %+v", got)
	}
	if mem.count() != 0 {
		t.Fatalf("facts %d after a failed job", mem.count())
	}

	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "second", time.Unix(51, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	// The worker can finish after this turn returns. The swap is still only
	// applied at a turn boundary, which is what the wait does once the job is ready.
	waitBlocks(t, k, 1)
	st, err = b.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Blocks) != 1 || st.Blocks[0].Text != "FULL" || strings.Contains(st.Blocks[0].Text, "PARTIAL") {
		t.Fatalf("blocks %+v", st.Blocks)
	}
	if st.Blocks[0].From != 1 || st.Blocks[0].To != 3 {
		t.Fatalf("range %+v", st.Blocks[0])
	}
	if mem.count() != 0 {
		t.Fatalf("compaction wrote %d facts to memory", mem.count())
	}

	// Crash after the block was saved and before the job file was removed.
	job = compactJob{From: 1, To: 3, Focus: defaultFocus, Status: jobReady, Summary: "FULL"}
	raw, err = fsutil.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if err := fsutil.WriteAtomic(filepath.Join(bot.Dir(b.Root, b.Name()), "compact-job.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	k2 := &Kernel{Bot: b, Provider: k.Provider, Compactor: comp, Memory: mem, Extractor: k.Extractor, Budget: k.Budget, MaxSteps: 2}
	if err := k2.Handle(context.Background(), link.TerminalEvent("u1", "third", time.Unix(52, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	st, err = b.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Blocks) != 1 || st.Blocks[0].Text != "FULL" {
		t.Fatalf("duplicate block %+v", st.Blocks)
	}
	if mem.count() != 0 {
		t.Fatalf("compaction wrote %d facts to memory", mem.count())
	}
	if calls := comp.ncalls(); calls != 2 {
		t.Fatalf("compactor calls %d, want the failed attempt and one retry", calls)
	}
}

func TestHardCompactStaysSynchronous(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	seedHistory(t, b, 3, 2000)
	comp := &delayCompactor{d: 200 * time.Millisecond}
	k := &Kernel{
		Bot: b, Provider: &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("ok")}}},
		Compactor: comp, Budget: compact.Budget{Ceiling: 40, KeepTail: 1}, MaxSteps: 2,
	}
	start := time.Now()
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "now", time.Unix(60, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
		t.Fatalf("hard compact returned in %s", elapsed)
	}
	st, err := b.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Blocks) != 1 || st.Blocks[0].Text != "SYNC" {
		t.Fatalf("blocks %+v", st.Blocks)
	}
	if calls := comp.ncalls(); calls != 1 {
		t.Fatalf("calls %d", calls)
	}
	raw, err := b.ConcatRaw()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), strings.Repeat("x", 2000)) {
		t.Fatal("transcript was rewritten")
	}
}

func seedHistory(t *testing.T, b *bot.Bot, n, size int) {
	t.Helper()
	body := strings.Repeat("x", size)
	for i := 0; i < n; i++ {
		if err := b.Append(bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(body)}}); err != nil {
			t.Fatal(err)
		}
	}
}

func tuneSoftBudget(t *testing.T, k *Kernel) {
	t.Helper()
	st, err := k.Bot.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	_, usage, err := k.assemble(migrate(st), "")
	if err != nil {
		t.Fatal(err)
	}
	margin := usage.History / 5
	if margin < 800 {
		margin = 800
	}
	k.Budget.Ceiling = usage.Total() + margin
	k.Budget.Target = usage.Total()
	k.Budget.KeepTail = 2
	if usage.Total() < k.target() {
		t.Fatalf("total %d under the soft target %d", usage.Total(), k.target())
	}
	if usage.Total() > k.Budget.Ceiling {
		t.Fatalf("already over the hard cap: %+v budget %d", usage, k.Budget.Ceiling)
	}
}

func readJobFile(t *testing.T, b *bot.Bot) compactJob {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(bot.Dir(b.Root, b.Name()), "compact-job.json"))
	if err != nil {
		t.Fatal(err)
	}
	var job compactJob
	if err := json.Unmarshal(raw, &job); err != nil {
		t.Fatal(err)
	}
	return job
}

func waitJob(t *testing.T, b *bot.Bot, status string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(filepath.Join(bot.Dir(b.Root, b.Name()), "compact-job.json"))
		if err == nil {
			var job compactJob
			if json.Unmarshal(raw, &job) == nil && job.Status == status {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job did not reach %s", status)
}

func waitBlocks(t *testing.T, k *Kernel, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		st, err := k.Bot.LoadState()
		if err == nil && len(st.Blocks) >= n {
			return
		}
		if err == nil {
			_, _, _ = k.applyReadyJob(migrate(st))
		}
		time.Sleep(10 * time.Millisecond)
	}
	st, _ := k.Bot.LoadState()
	t.Fatalf("blocks %+v, want %d", st.Blocks, n)
}

func waitCalls(t *testing.T, c *flakyCompactor, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.ncalls() >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("calls %d, want %d", c.ncalls(), n)
}

func waitCompactor(t *testing.T, c *gateCompactor) {
	t.Helper()
	select {
	case <-c.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("background compactor did not start")
	}
}

type gateCompactor struct {
	release chan struct{}
	entered chan struct{}
	once    sync.Once
	mu      sync.Mutex
	calls   int
	turns   []provider.Message
}

func (g *gateCompactor) Compact(_ context.Context, _ string, turns []provider.Message) (string, error) {
	g.mu.Lock()
	g.calls++
	g.turns = append([]provider.Message(nil), turns...)
	g.mu.Unlock()
	g.once.Do(func() { close(g.entered) })
	<-g.release
	return "SUM", nil
}

func (g *gateCompactor) ncalls() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

func (g *gateCompactor) snapshot() []provider.Message {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]provider.Message(nil), g.turns...)
}

type noteCompactor struct {
	mu    sync.Mutex
	calls int
}

func (n *noteCompactor) Compact(context.Context, string, []provider.Message) (string, error) {
	n.mu.Lock()
	n.calls++
	n.mu.Unlock()
	return "SUM", nil
}

type flakyCompactor struct {
	mu    sync.Mutex
	calls int
	fail  int
}

func (f *flakyCompactor) Compact(context.Context, string, []provider.Message) (string, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	fail := f.fail
	f.mu.Unlock()
	if n <= fail {
		return "", errors.New("compact failed")
	}
	return "FULL", nil
}

func (f *flakyCompactor) ncalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type delayCompactor struct {
	d     time.Duration
	mu    sync.Mutex
	calls int
}

func (d *delayCompactor) Compact(context.Context, string, []provider.Message) (string, error) {
	d.mu.Lock()
	d.calls++
	d.mu.Unlock()
	time.Sleep(d.d)
	return "SYNC", nil
}

func (d *delayCompactor) ncalls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

type holdChat struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (h *holdChat) Chat(context.Context, provider.Request, func(string)) (provider.Message, error) {
	h.once.Do(func() { close(h.entered) })
	<-h.release
	return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("ok")}, nil
}

type stubExtract struct{ facts []memory.Fact }

func (s stubExtract) Extract(context.Context, string, []provider.Message) ([]memory.Fact, error) {
	return s.facts, nil
}

type factMem struct {
	mu    sync.Mutex
	facts []memory.Fact
}

func (m *factMem) Remember(_ context.Context, f memory.Fact) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.facts = append(m.facts, f)
	return nil
}

func (m *factMem) Recall(context.Context, memory.Query) ([]memory.Fact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]memory.Fact(nil), m.facts...), nil
}

func (m *factMem) Forget(context.Context, memory.Query, string) error { return nil }

func (m *factMem) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.facts)
}
