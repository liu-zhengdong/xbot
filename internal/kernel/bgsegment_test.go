package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
)

// loopProvider keeps asking for the wait tool, so every segment runs to its
// step cap and the turn never produces a final answer of its own.
type loopProvider struct{ calls int }

func (p *loopProvider) Chat(_ context.Context, _ provider.Request, _ func(string)) (provider.Message, error) {
	p.calls++
	// Each step is a different call with enough new text that the circuit
	// breaker does not treat this cap-filling loop as a stuck loop.
	return provider.Message{
		Role:    provider.RoleAssistant,
		Content: fmt.Sprintf("step %d %s", p.calls, strings.Repeat("x", 2200)),
		ToolCalls: []provider.ToolCall{{
			ID: fmt.Sprintf("c%d", p.calls), Type: "function",
			Function: provider.FunctionCall{Name: "wait", Arguments: fmt.Sprintf(`{"n":%d}`, p.calls)},
		}},
	}, nil
}

// waitTool blocks on its first call, which is what gets the turn forked to the
// background; the release channel makes every later call return at once.
// The turn context ends the wait, so a fork that never arrives fails the test
// instead of blocking until the process timeout.
type waitTool struct{ release chan struct{} }

func (*waitTool) Name() string                                  { return "wait" }
func (*waitTool) Description() string                           { return "wait" }
func (*waitTool) Parameters() any                               { return map[string]any{"type": "object"} }
func (*waitTool) Confirm(json.RawMessage) (bool, string, error) { return false, "wait", nil }
func (w *waitTool) Run(ctx context.Context, _ json.RawMessage) (string, error) {
	select {
	case <-w.release:
		return "ok", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// TestWaitToolStopsWhenContextEnds is the regression for runBackground hanging
// in waitTool until the process timeout. The tool used to block on the release
// channel and ignore cancellation, so a turn that never forked never reached
// close(rel).
func TestWaitToolStopsWhenContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	w := &waitTool{release: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := w.Run(ctx, nil)
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("wait tool err %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wait tool still blocked on release after context ended")
	}
}

// segmentKernel builds a kernel whose turn forks to the background at once, so
// the segment loop runs there.
func segmentKernel(t *testing.T) (*Kernel, chan struct{}, *loopProvider) {
	t.Helper()
	k, _, _ := forkKernel(t)
	rel := make(chan struct{})
	p := &loopProvider{}
	k.Links = []link.Link{link.Terminal{}, oneToolLink{&waitTool{release: rel}}}
	k.Provider = p
	k.MaxSteps = 2
	k.ForegroundMaxTools = 1
	return k, rel, p
}

// runBackground forks the turn, lets the tool through, and waits until that
// background's fork_end event is in the inbox. It returns the background id.
func runBackground(t *testing.T, k *Kernel, rel chan struct{}, task string) string {
	t.Helper()
	ev := link.TerminalEvent("u1", task, time.Unix(10, 0).UTC())
	if err := k.Inbox.Post(ev); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), backgroundWait)
	defer cancel()
	if err := k.Handle(ctx, ev); err != nil {
		t.Fatalf("turn did not fork within %s: %v", backgroundWait, err)
	}
	if n := len(k.Backgrounds()); n != 1 {
		t.Fatalf("backgrounds %d; the turn did not fork", n)
	}
	id := k.Backgrounds()[0].ID
	close(rel)
	waitNoBackgrounds(t, k, id)
	return id
}

func endEvent(t *testing.T, k *Kernel, id string) string {
	t.Helper()
	return waitForkEnd(t, k, id).Content
}

func continueNotes(t *testing.T, k *Kernel) int {
	t.Helper()
	entries, err := k.Bot.Entries()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if e.Branch != "" && e.Message.Content == continueNote {
			n++
		}
	}
	return n
}

// TestBackgroundContinuesUntilSegmentCap is the whole point of the feature: a
// background that ends every segment at its step cap keeps going by itself,
// segment by segment, and reports where it stopped.
func TestBackgroundContinuesUntilSegmentCap(t *testing.T) {
	k, rel, p := segmentKernel(t)
	id := runBackground(t, k, rel, "一直做")
	text := endEvent(t, k, id)
	if !strings.Contains(text, "4 段") {
		t.Fatalf("event does not report the segments %q", text)
	}
	if !strings.Contains(text, "段数上限") {
		t.Fatalf("event does not say where it stopped %q", text)
	}
	// Four segments of two steps each, and one closing summary per segment.
	if p.calls != 12 {
		t.Fatalf("model calls %d, want 12 (4 segments x (2 steps + 1 summary))", p.calls)
	}
	if n := continueNotes(t, k); n != 3 {
		t.Fatalf("continue notes %d, want 3", n)
	}
}

// quietProvider fills every segment with a distinct tiny tool call, so the only
// pattern that can stop it before the segment cap is diminishing returns.
type quietProvider struct{ calls int }

func (p *quietProvider) Chat(_ context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	p.calls++
	if len(req.Tools) == 0 {
		return provider.Message{Role: provider.RoleAssistant, Content: "summary"}, nil
	}
	return provider.Message{
		Role:    provider.RoleAssistant,
		Content: fmt.Sprintf("q%d", p.calls),
		ToolCalls: []provider.ToolCall{{
			ID: fmt.Sprintf("c%d", p.calls), Type: "function",
			Function: provider.FunctionCall{Name: "wait", Arguments: fmt.Sprintf(`{"n":%d}`, p.calls)},
		}},
	}, nil
}

// TestQuietContinuationsHardStop is the kernel-level diminishing-returns
// regression: five quiet cap-continuations stop the background before a high
// segment cap, and the trace records the code.
func TestQuietContinuationsHardStop(t *testing.T) {
	k, rel, _ := segmentKernel(t)
	p := &quietProvider{}
	k.Provider = p
	k.MaxSegments = 8
	id := runBackground(t, k, rel, "安静地续")
	text := endEvent(t, k, id)
	if !strings.Contains(text, StopDiminishingReturns) {
		t.Fatalf("event %q", text)
	}
	if strings.Contains(text, "段数上限") {
		t.Fatalf("segment cap stopped it instead %q", text)
	}
	if !strings.Contains(text, "5 段") {
		t.Fatalf("event segments %q", text)
	}
	traces, err := k.Bot.Traces()
	if err != nil {
		t.Fatal(err)
	}
	var stops []bot.Trace
	for _, tr := range traces {
		if tr.Kind == bot.TraceStop {
			stops = append(stops, tr)
		}
	}
	if len(stops) != 1 || stops[0].Reason != StopDiminishingReturns {
		t.Fatalf("stop traces %+v of %+v", stops, traces)
	}
	entries, err := k.Bot.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var note bool
	for _, e := range entries {
		if strings.Contains(e.Message.Content, "<stop_reason>"+StopDiminishingReturns+"</stop_reason>") {
			note = true
		}
	}
	if !note {
		t.Fatal("transcript has no stop reason")
	}
	if n := continueNotes(t, k); n != 4 {
		t.Fatalf("continue notes %d, want 4", n)
	}
}

// TestBackgroundStopsAtWallClockCap checks the cumulative wall-clock budget
// stops the continuation before the segment cap does.
func TestBackgroundStopsAtWallClockCap(t *testing.T) {
	k, rel, _ := segmentKernel(t)
	k.BackgroundMaxTime = time.Nanosecond
	id := runBackground(t, k, rel, "慢慢做")
	text := endEvent(t, k, id)
	if !strings.Contains(text, "墙钟") {
		t.Fatalf("event %q", text)
	}
	if !strings.Contains(text, "1 段") {
		t.Fatalf("event shows more than one segment %q", text)
	}
	if n := continueNotes(t, k); n != 0 {
		t.Fatalf("continue notes %d, want none", n)
	}
}

// TestContinueSegmentLimits pins the decision itself, including the cumulative
// step cap, which the loop only reaches when queued controls run extra steps.
func TestContinueSegmentLimits(t *testing.T) {
	// Every turn here starts now, so only the limit under test can stop it.
	start := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	k := &Kernel{MaxSteps: 2, Now: func() time.Time { return start }}
	if more, reason := k.continueSegmentLocked(&turn{segments: 1}, false); more || reason != "" {
		t.Fatalf("a segment that did not hit the cap continued: %v %q", more, reason)
	}
	tr := &turn{segments: 1, capStopped: true, stepsUsed: 2, started: start}
	if more, reason := k.continueSegmentLocked(tr, false); !more || reason != "" || tr.segments != 2 {
		t.Fatalf("under every cap: %v %q segments %d", more, reason, tr.segments)
	}
	if more, reason := k.continueSegmentLocked(&turn{segments: 4, capStopped: true, stepsUsed: 4, started: start}, false); more || !strings.Contains(reason, "段数上限") {
		t.Fatalf("segment cap: %v %q", more, reason)
	}
	if more, reason := k.continueSegmentLocked(&turn{segments: 2, capStopped: true, stepsUsed: 8, started: start}, false); more || !strings.Contains(reason, "步数上限") {
		t.Fatalf("step cap: %v %q", more, reason)
	}
	if k.totalStepCap() != 8 {
		t.Fatalf("total step cap %d, want 4 x 2", k.totalStepCap())
	}

	start = time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	timed := &Kernel{MaxSteps: 2, BackgroundMaxTime: time.Minute, Now: func() time.Time { return start }}
	over := &turn{segments: 1, capStopped: true, stepsUsed: 2, started: start.Add(-2 * time.Minute)}
	if more, reason := timed.continueSegmentLocked(over, false); more || !strings.Contains(reason, "墙钟") {
		t.Fatalf("wall clock: %v %q", more, reason)
	}
	// A negative budget turns the wall-clock limit off.
	off := &Kernel{MaxSteps: 2, BackgroundMaxTime: -1, Now: func() time.Time { return start }}
	if more, _ := off.continueSegmentLocked(over, false); !more {
		t.Fatal("a negative wall-clock budget should not stop the continuation")
	}
}
