package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/config"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

func TestBatchBEnvironmentCurrentValues(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	now := time.Unix(1200, 0).UTC()
	p := &scripted{}
	for i := 0; i < 5; i++ {
		p.resps = append(p.resps, provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("ok")})
	}
	k := &Kernel{Bot: b, Provider: p, Memory: memory.NopStore{}, Now: func() time.Time { return now }}
	appendCard(t, b, "card", "shell", "echo hi", bot.ApprovalPending, now)
	for i := 0; i < 3; i++ {
		if err := k.Handle(context.Background(), link.TerminalEvent("u1", fmt.Sprint(i), now.Add(time.Duration(i)*time.Second))); err != nil {
			t.Fatal(err)
		}
	}
	msgs := p.reqs[2].Messages
	times, cards := 0, 0
	for _, m := range msgs {
		if e, ok := decodeEnvironment(m.Text()); ok {
			if e.Set["time"] != nil {
				times++
			}
			if string(e.Set["pending_approvals"]) == "1" {
				cards++
			}
		}
	}
	if times != 1 || cards != 1 {
		t.Fatalf("same-minute publications time=%d cards=%d", times, cards)
	}
	raw, _ := b.ConcatRaw()
	appendCard(t, b, "card", "shell", "echo hi", bot.ApprovalApproved, now)
	now = now.Add(time.Minute)
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "cleared", now)); err != nil {
		t.Fatal(err)
	}
	msgs = p.reqs[3].Messages
	cleared := false
	for _, m := range msgs {
		if e, ok := decodeEnvironment(m.Text()); ok && string(e.Set["pending_approvals"]) == "null" {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("missing explicit card removal in model request")
	}
	env := replayEnvironment(msgs, "")
	if _, ok := env.Fields["pending_approvals"]; ok {
		t.Fatal("cleared card still current")
	}
	if environmentGet[string](env, "time") != k.localStamp() {
		t.Fatal("clock did not advance")
	}
	after, _ := b.ConcatRaw()
	if !strings.HasPrefix(string(after), string(raw)) {
		t.Fatal("history changed")
	}
	// Cover the old publications, then the next request must see a fresh,
	// current snapshot without bringing the cleared card back.
	n, _ := b.Count()
	st, _ := b.LoadState()
	st.Blocks = []bot.SummaryBlock{{From: 1, To: n, Text: "prior turns"}}
	if err := b.SaveState(st); err != nil {
		t.Fatal(err)
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "after compact", now)); err != nil {
		t.Fatal(err)
	}
	msgs = p.reqs[4].Messages
	snapshots := 0
	for _, m := range msgs {
		if e, ok := decodeEnvironment(m.Text()); ok && e.Snapshot != "" {
			snapshots++
		}
	}
	if snapshots != 1 {
		t.Fatalf("snapshots=%d", snapshots)
	}
	env = replayEnvironment(msgs, st.MemSnap)
	if environmentGet[string](env, "time") != k.localStamp() || env.Fields["pending_approvals"] != nil {
		t.Fatal("snapshot not current")
	}
}

func TestBatchBPastToolResultBeforeSoftTarget(t *testing.T) {
	b := testBot(t)
	receipt := encodeEnvironment(environmentMessage{Memory: &memoryChange{Add: []string{"[Bot] remembered"}}})
	original := strings.Repeat("large result ", 240) + "\n\n" + receipt
	entries := []bot.Entry{
		{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("old input")}},
		{Message: assistantCall("read", "", "write_file", `{}`)},
		{Message: batchToolReply("read", original)},
		{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("done")}},
		{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("new input")}},
		{Message: assistantCall("current", "", "read_file", `{}`)},
		{Message: batchToolReply("current", strings.Repeat("new result ", 300))},
	}
	for _, e := range entries {
		if err := b.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	k := &Kernel{Bot: b}
	msgs, usage, err := k.assemble(bot.State{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if usage.Total() >= k.target() {
		t.Fatal("fixture is not below soft target")
	}
	old, newest := "", ""
	for _, m := range msgs {
		if m.CallID() == "read" {
			old = m.Text()
		}
		if m.CallID() == "current" {
			newest = m.Text()
		}
	}
	if !strings.Contains(old, "expand with from=3 to=3") || strings.Contains(old, "large result") {
		t.Fatalf("not omitted: %.200s", old)
	}
	if !strings.HasPrefix(newest, "new result ") {
		t.Fatal("current turn result omitted")
	}
	if !strings.Contains(strings.Join(replayEnvironment(msgs, "").Memory, " "), "remembered") {
		t.Fatal("memory receipt lost")
	}
	got, err := b.Slice(3, 3)
	if err != nil || got[0].Message.Text() != original {
		t.Fatal("original changed")
	}
}

func TestBatchBFitSummaryBeforeOmit(t *testing.T) {
	for _, enabled := range []string{"", "false", "true"} {
		t.Run("flag="+enabled, func(t *testing.T) {
			t.Setenv("XBOT_FIT_SUMMARY_BEFORE_OMIT", enabled)
			c := &batchFactCompactor{}
			k := &Kernel{Compactor: c, Budget: compact.Budget{Target: 2000, Ceiling: 10000}}
			msgs := []provider.Message{assistantCall("a", "", "read_file", `{}`), batchToolReply("a", "important detail 1234 "+strings.Repeat("x", 9000)), assistantCall("b", "next", "read_file", `{}`), batchToolReply("b", "latest")}
			f := newTurnFit(0, nil)
			got := k.fit(context.Background(), nil, f, provider.Request{Messages: msgs}, false)
			if enabled == "true" {
				if c.calls != 1 || !strings.HasPrefix(got.Messages[0].Text(), "<turn_progress") || !strings.Contains(got.Messages[0].Text(), "important detail 1234") {
					t.Fatal("pre-omit summary did not run")
				}
				if len(c.got) < 2 || !strings.Contains(c.got[1].Text(), "important detail 1234") {
					t.Fatal("summary did not read originals")
				}
			} else if c.calls != 0 || len(f.omitted) != 1 {
				t.Fatalf("default changed: calls=%d omitted=%d", c.calls, len(f.omitted))
			}
			if got.Messages[len(got.Messages)-1].Text() != "latest" {
				t.Fatal("latest step cut")
			}
		})
	}
}

func TestBatchBIdleCompactPolicy(t *testing.T) {
	for _, off := range []string{"false", "0", "off", "关"} {
		t.Run(off, func(t *testing.T) {
			t.Setenv("XBOT_IDLE_COMPACT_MIN_TOKENS", off)
			n, d := idleCompactPolicy()
			if n != 0 || d != 0 {
				t.Fatal("off ignored")
			}
		})
	}
	t.Setenv("XBOT_IDLE_COMPACT_MIN_TOKENS", "123")
	t.Setenv("XBOT_IDLE_COMPACT_AFTER", "42m")
	n, d := idleCompactPolicy()
	if n != 123 || d != 42*time.Minute {
		t.Fatalf("policy %d %s", n, d)
	}
}

func TestBatchBIdleCompactDoesNotBlock(t *testing.T) {
	t.Setenv("XBOT_IDLE_COMPACT_MIN_TOKENS", "1000")
	t.Setenv("XBOT_IDLE_COMPACT_AFTER", "50m")
	b := testBot(t, link.NameTerminal)
	now := time.Now().UTC()
	for i := 0; i < 20; i++ {
		if err := b.Append(bot.Entry{Time: now.Add(-time.Hour), Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(strings.Repeat("x", 3000))}}); err != nil {
			t.Fatal(err)
		}
	}
	c := &gateCompactor{entered: make(chan struct{}), release: make(chan struct{})}
	k := &Kernel{Bot: b, Budget: compact.Budget{Target: 200000, Ceiling: 868000}, Compactor: c, Provider: &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("ok")}}}, Memory: memory.NopStore{}}
	k.MaybeIdleCompact(now.Add(-20 * time.Minute))
	job, _ := k.readJob()
	if job.Status != "" {
		t.Fatal("started too early")
	}
	t.Setenv("XBOT_IDLE_COMPACT_MIN_TOKENS", "false")
	k.MaybeIdleCompact(now)
	job, _ = k.readJob()
	if job.Status != "" {
		t.Fatal("disabled still starts")
	}
	t.Setenv("XBOT_IDLE_COMPACT_MIN_TOKENS", "999999")
	k.MaybeIdleCompact(now)
	job, _ = k.readJob()
	if job.Status != "" {
		t.Fatal("below threshold still starts")
	}
	t.Setenv("XBOT_IDLE_COMPACT_MIN_TOKENS", "1000")
	k.MaybeIdleCompact(now)
	waitCompactor(t, c)
	// The ready/running file deduplicates even a different kernel/process pass.
	k2 := &Kernel{Bot: b, Compactor: c}
	k2.MaybeIdleCompact(now)
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		done <- k.Handle(context.Background(), link.TerminalEvent("u1", "while idle compact runs", now))
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("foreground blocked on idle summary")
	}
	t.Logf("foreground while summary blocked: %s", time.Since(start))
	if c.ncalls() != 1 {
		t.Fatalf("duplicate summaries %d", c.ncalls())
	}
	close(c.release)
	waitJob(t, b, jobReady)
}

func TestBatchBBindingCompactsOrRefuses(t *testing.T) {
	for _, canCompact := range []bool{true, false} {
		t.Run(fmt.Sprint(canCompact), func(t *testing.T) {
			b := testBot(t, link.NameTerminal)
			if err := config.Add(b.Root, config.Provider{Name: "p", BaseURL: "http://localhost/v1", APIKeyEnv: "XBOT_BATCH_B_TEST_KEY"}, config.ModelSpec{Name: "large", Context: 1000000}); err != nil {
				t.Fatal(err)
			}
			if err := b.SetBinding("p", "large"); err != nil {
				t.Fatal(err)
			}
			seedHistory(t, b, 30, 4000)
			k := &Kernel{Bot: b, Memory: memory.NopStore{}}
			c := &recCompactor{}
			if canCompact {
				k.Compactor = c
			}
			err := k.PrepareBinding(context.Background(), "p", "small", 40000)
			if canCompact {
				if err != nil || c.calls != 1 {
					t.Fatalf("compact calls %d err %v", c.calls, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "上下文仍超新窗口") {
				t.Fatalf("expected clear refusal, got %v", err)
			}
			if b.Config().Model != "large" {
				t.Fatal("preflight changed binding")
			}
		})
	}
}

// Keep the JSON set semantics explicit: absent does not clear, null does.
func TestBatchBTaskRemovalProjection(t *testing.T) {
	prev := environment{Fields: map[string]json.RawMessage{"task:one": json.RawMessage(`{"id":"one","status":"running"}`)}}
	text := environmentDelta(prev, environment{Fields: map[string]json.RawMessage{}}, "", "")
	p, ok := decodeEnvironment(text)
	if !ok || string(p.Set["task:one"]) != "null" {
		t.Fatal(text)
	}
	applyEnvironment(&prev, p, nil)
	if len(prev.Fields) != 0 {
		t.Fatal("task retained")
	}
}

func batchToolReply(id, text string) provider.Message {
	return provider.Message{Role: provider.RoleTool, Content: provider.ToolResult(id, text)}
}

func TestBatchBBlobOmissionKeepsReceiptAndExpand(t *testing.T) {
	b := testBot(t)
	receipt := encodeEnvironment(environmentMessage{Memory: &memoryChange{Add: []string{"[Bot] blob fact"}}})
	text := strings.Repeat("body ", 1000) + "\n\n" + receipt
	result := batchToolReply("c", text)
	result.Name = "write_file"
	for _, m := range []provider.Message{assistantCall("c", "", "write_file", `{}`), result, {Role: provider.RoleUser, Content: provider.TextContent("next")}} {
		if err := b.Append(bot.Entry{Message: m}); err != nil {
			t.Fatal(err)
		}
	}
	k := &Kernel{Bot: b}
	msgs, _, err := k.assemble(bot.State{}, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range msgs {
		if m.CallID() == "c" {
			found = strings.Contains(m.Text(), "expand with from=2 to=2")
		}
	}
	if !found {
		t.Fatal("blob preview did not become expand placeholder")
	}
	if !strings.Contains(strings.Join(replayEnvironment(msgs, "").Memory, " "), "blob fact") {
		t.Fatal("blob receipt lost")
	}
	full, err := k.ExpandBot("", 2, 2)
	if err != nil || !strings.Contains(full, text) {
		t.Fatal("expand lost original")
	}
}

func TestBatchBFitSummaryFailureFallsBackOnce(t *testing.T) {
	t.Setenv("XBOT_FIT_SUMMARY_BEFORE_OMIT", "true")
	c := &flakyCompactor{fail: 1}
	k := &Kernel{Compactor: c, Budget: compact.Budget{Target: 2000, Ceiling: 10000}}
	msgs := []provider.Message{assistantCall("a", "", "read_file", `{}`), batchToolReply("a", strings.Repeat("x", 9000)), assistantCall("b", "next", "read_file", `{}`), batchToolReply("b", "latest")}
	f := newTurnFit(0, nil)
	got := k.fit(context.Background(), nil, f, provider.Request{Messages: msgs}, false)
	if c.ncalls() != 1 || len(f.omitted) != 1 || !strings.Contains(got.Messages[1].Text(), "result left out") {
		t.Fatal("failed summary did not fall back once to omit")
	}
}

func TestBatchBBindingRefusesAfterInsufficientCompaction(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	seedHistory(t, b, 30, 4000)
	c := &recCompactor{}
	k := &Kernel{Bot: b, Compactor: c, Memory: memory.NopStore{}}
	err := k.PrepareBinding(context.Background(), "p", "tiny", 8000)
	if err == nil || !strings.Contains(err.Error(), "上下文仍超新窗口") || c.calls != 1 {
		t.Fatalf("calls=%d err=%v", c.calls, err)
	}
	if b.Config().Provider != "" || k.Budget.Target != 0 {
		t.Fatal("failed preflight changed binding or budget")
	}
	st, err := b.LoadState()
	if err != nil || len(st.Blocks) != 1 {
		t.Fatal("committed summary was lost")
	}
}

// Scripted summary preserves a fact only if it actually received the original.
type batchFactCompactor struct{ recCompactor }

func (c *batchFactCompactor) Compact(ctx context.Context, prior string, turns []provider.Message) (string, error) {
	_, err := c.recCompactor.Compact(ctx, prior, turns)
	for _, m := range turns {
		if strings.Contains(m.Text(), "important detail 1234") {
			return "Read file: important detail 1234; expand the original range for more.", err
		}
	}
	return "detail was not read", err
}

func TestBatchBForkHandoffKeepsPastToolBodies(t *testing.T) {
	b := testBot(t)
	original := strings.Repeat("handoff-source-detail ", 240)
	entries := []bot.Entry{
		{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("old input")}},
		{Message: assistantCall("read", "", "read_file", `{}`)},
		{Message: batchToolReply("read", original)},
		{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("done")}},
		{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("new input")}},
	}
	for _, e := range entries {
		if err := b.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	var saw []provider.Message
	k := &Kernel{Bot: b, Memory: memory.NopStore{}, Compactor: &handoffCaptureCompactor{fn: func(_ context.Context, _ string, turns []provider.Message) (string, error) {
		saw = append([]provider.Message(nil), turns...)
		return "handoff summary", nil
	}}}
	if _, err := k.handoff(context.Background(), "new role"); err != nil {
		t.Fatal(err)
	}
	if len(saw) == 0 {
		t.Fatal("compactor saw no turns")
	}
	joined := ""
	for _, m := range saw {
		joined += m.Text()
	}
	if !strings.Contains(joined, "handoff-source-detail") {
		t.Fatalf("handoff source lost tool body: %.200s", joined)
	}
	if strings.Contains(joined, "result left out") || strings.Contains(joined, "expand with from=") {
		t.Fatalf("handoff source still has expand placeholders: %.200s", joined)
	}
	msgs, _, err := k.assemble(bot.State{}, "")
	if err != nil {
		t.Fatal(err)
	}
	foundOmit := false
	for _, m := range msgs {
		if m.CallID() == "read" && strings.Contains(m.Text(), "expand with from=") {
			foundOmit = true
			break
		}
	}
	if !foundOmit {
		t.Fatal("assemble should still omit past large tool results")
	}
}

type handoffCaptureCompactor struct {
	fn func(context.Context, string, []provider.Message) (string, error)
}

func (c *handoffCaptureCompactor) Compact(ctx context.Context, prior string, turns []provider.Message) (string, error) {
	return c.fn(ctx, prior, turns)
}
