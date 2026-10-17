package kernel

import (
	"context"
	"strings"
	"testing"
	"time"

	"encoding/json"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

func assistantCall(id, text, name, args string) provider.Message {
	return provider.Message{
		Role:    provider.RoleAssistant,
		Content: text,
		ToolCalls: []provider.ToolCall{{
			ID: id, Type: "function",
			Function: provider.FunctionCall{Name: name, Arguments: args},
		}},
	}
}

func toolResult(id, name, content string) provider.Message {
	return provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: name, Content: content}
}

func TestBreakerTripsTwice(t *testing.T) {
	big := strings.Repeat("x", 2100)
	cases := []struct {
		name   string
		steps  [][]provider.Message
		reason string
	}{
		{
			name: "repeated output",
			steps: [][]provider.Message{
				{assistantCall("1", "same words", "echo", `{"n":"1"}`), toolResult("1", "echo", "ok")},
				{assistantCall("2", "same words", "echo", `{"n":"2"}`), toolResult("2", "echo", "ok")},
				{assistantCall("3", "same words", "echo", `{"n":"3"}`), toolResult("3", "echo", "ok")},
			},
			reason: StopRepeatedOutput,
		},
		{
			name: "same tool args",
			steps: [][]provider.Message{
				{assistantCall("1", "a "+big, "echo", `{"n":"1"}`), toolResult("1", "echo", "ok")},
				{assistantCall("2", "b "+big, "echo", `{"n":"1"}`), toolResult("2", "echo", "ok")},
				{assistantCall("3", "c "+big, "echo", `{"n":"1"}`), toolResult("3", "echo", "ok")},
				{assistantCall("4", "d "+big, "echo", `{"n":"1"}`), toolResult("4", "echo", "ok")},
			},
			reason: StopSameToolArgs,
		},
		{
			name: "file flip flop",
			steps: [][]provider.Message{
				{assistantCall("1", "a "+big, "write_file", `{"path":"n.txt","content":"A"}`), toolResult("1", "write_file", "ok")},
				{assistantCall("2", "b "+big, "write_file", `{"path":"n.txt","content":"B"}`), toolResult("2", "write_file", "ok")},
				{assistantCall("3", "c "+big, "write_file", `{"path":"n.txt","content":"A"}`), toolResult("3", "write_file", "ok")},
				{assistantCall("4", "d "+big, "write_file", `{"path":"n.txt","content":"B"}`), toolResult("4", "write_file", "ok")},
			},
			reason: StopFileFlipFlop,
		},
		{
			name: "polling failure",
			steps: [][]provider.Message{
				{assistantCall("1", "a "+big, "http_request", `{"url":"http://down"}`), toolResult("1", "http_request", "error: connection refused")},
				{assistantCall("2", "b "+big, "http_request", `{"url":"http://down"}`), toolResult("2", "http_request", "error: connection refused")},
				{assistantCall("3", "c "+big, "http_request", `{"url":"http://down"}`), toolResult("3", "http_request", "error: connection refused")},
				{assistantCall("4", "d "+big, "http_request", `{"url":"http://down"}`), toolResult("4", "http_request", "error: connection refused")},
			},
			reason: StopPollingFailure,
		},
		{
			name: "denied approvals",
			steps: [][]provider.Message{
				{assistantCall("1", "a "+big, "shell", `{"command":"rm a"}`), toolResult("1", "shell", "denied by user")},
				{assistantCall("2", "b "+big, "shell", `{"command":"rm b"}`), toolResult("2", "shell", "denied by user")},
				{assistantCall("3", "c "+big, "shell", `{"command":"rm c"}`), toolResult("3", "shell", "denied by user")},
				{assistantCall("4", "d "+big, "shell", `{"command":"rm d"}`), toolResult("4", "shell", "denied by user")},
				{assistantCall("5", "e "+big, "shell", `{"command":"rm e"}`), toolResult("5", "shell", "denied by user")},
				{assistantCall("6", "f "+big, "shell", `{"command":"rm f"}`), toolResult("6", "shell", "denied by user")},
			},
			reason: StopDeniedApprovals,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			br := newBreaker()
			var reminded bool
			for i, step := range tc.steps {
				remind, stop, reason := br.Observe(step)
				last := i == len(tc.steps)-1
				if !last {
					if stop {
						t.Fatalf("step %d stopped early (%s)", i, reason)
					}
					if remind != "" {
						if reminded {
							t.Fatalf("step %d reminded twice", i)
						}
						reminded = true
						if !strings.Contains(remind, "<system-reminder>") || !strings.Contains(remind, tc.reason) {
							t.Fatalf("reminder %q", remind)
						}
					}
					continue
				}
				if !stop || reason != tc.reason {
					t.Fatalf("step %d stop=%v reason=%q, want %s", i, stop, reason, tc.reason)
				}
				if !reminded {
					t.Fatal("hard stop without a prior reminder")
				}
			}
		})
	}
}

type echoTool struct{}

func (echoTool) Name() string        { return "echo" }
func (echoTool) Description() string { return "echo" }
func (echoTool) Parameters() any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"n": map[string]any{"type": "string"},
	}}
}
func (echoTool) Confirm(json.RawMessage) (bool, string, error) { return false, "echo", nil }
func (echoTool) Run(context.Context, json.RawMessage) (string, error) {
	return "ok", nil
}

type echoLink struct{}

func (echoLink) Name() string        { return "echo" }
func (echoLink) Tools() []tools.Tool { return []tools.Tool{echoTool{}} }

func TestBreakerStopWritesTrace(t *testing.T) {
	b := testBot(t, link.NameTerminal, "echo")
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{
		assistantCall("c1", "same words", "echo", `{"n":"1"}`),
		assistantCall("c2", "same words", "echo", `{"n":"2"}`),
		assistantCall("c3", "same words", "echo", `{"n":"3"}`),
		{Role: provider.RoleAssistant, Content: "I stopped because the reply repeated."},
	}}
	k := &Kernel{
		Bot: b, Provider: p, Memory: memory.NopStore{}, MaxSteps: 8,
		Links: []link.Link{link.Terminal{}, echoLink{}},
	}
	ev := link.TerminalEvent("u1", "go", time.Unix(9, 0).UTC())
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) < 3 {
		t.Fatalf("requests %d", len(p.reqs))
	}
	// The reminder is attached before the third model call.
	saw := false
	for _, m := range p.reqs[2].Messages {
		if strings.Contains(m.Content, "<system-reminder>") && strings.Contains(m.Content, StopRepeatedOutput) {
			saw = true
		}
	}
	if !saw {
		t.Fatal("first trip did not remind before the next model call")
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var note bool
	for _, e := range entries {
		if strings.Contains(e.Message.Content, "<stop_reason>"+StopRepeatedOutput+"</stop_reason>") {
			note = true
		}
	}
	if !note {
		t.Fatal("transcript has no stop reason")
	}
	traces, err := b.Traces()
	if err != nil {
		t.Fatal(err)
	}
	var stops []bot.Trace
	for _, tr := range traces {
		if tr.Kind == bot.TraceStop {
			stops = append(stops, tr)
		}
	}
	if len(stops) != 1 || stops[0].Reason != StopRepeatedOutput {
		t.Fatalf("stop traces %+v of %+v", stops, traces)
	}
}

// TestToolStepsAreNotDiminishingReturns guards the diminishing-returns counter.
// Distinct short tool calls are ordinary work and must not remind or stop.
// f12fd9b has no breaker. The first cut of this milestone treated every short
// step as a continuation and reminded on the fourth call.
func TestToolStepsAreNotDiminishingReturns(t *testing.T) {
	br := newBreaker()
	for i := 1; i <= 8; i++ {
		id := string(rune('a' + i - 1))
		step := []provider.Message{
			assistantCall(id, "", "echo", `{"n":"`+id+`"}`),
			toolResult(id, "echo", "ok"),
		}
		remind, stop, reason := br.Observe(step)
		if remind != "" || stop {
			t.Fatalf("step %d remind=%q stop=%v reason=%q", i, remind, stop, reason)
		}
	}
}

// TestDiminishingReturnsCountsContinuations trips only after three quiet
// continuation segments, then hard-stops when a fourth would start. Small
// steps before the first continuation are not counted.
func TestDiminishingReturnsCountsContinuations(t *testing.T) {
	br := newBreaker()
	for i := 0; i < 6; i++ {
		id := string(rune('a' + i))
		remind, stop, reason := br.Observe([]provider.Message{
			assistantCall(id, "open "+id, "echo", `{"n":"`+id+`"}`),
			toolResult(id, "echo", "ok"),
		})
		if remind != "" || stop {
			t.Fatalf("opening step %d remind=%q stop=%v %s", i, remind, stop, reason)
		}
	}
	quiet := func(n int) {
		t.Helper()
		id := string(rune('m' + n))
		if _, stop, reason := br.Observe([]provider.Message{
			assistantCall(id, "c "+id, "echo", `{"n":"`+id+`"}`),
			toolResult(id, "echo", "ok"),
		}); stop {
			t.Fatalf("observe %d stopped (%s)", n, reason)
		}
	}
	for c := 1; c <= 3; c++ {
		if remind, stop, reason := br.BeginContinuation(); stop || remind != "" {
			t.Fatalf("begin %d remind=%q stop=%v %s", c, remind, stop, reason)
		}
		quiet(c)
	}
	remind, stop, reason := br.BeginContinuation()
	if stop || !strings.Contains(remind, StopDiminishingReturns) {
		t.Fatalf("third close remind=%q stop=%v %s", remind, stop, reason)
	}
	quiet(4)
	remind, stop, reason = br.BeginContinuation()
	if !stop || reason != StopDiminishingReturns || remind != "" {
		t.Fatalf("fourth close remind=%q stop=%v reason=%q", remind, stop, reason)
	}
}

// TestLargeContinuationsDoNotTrip keeps a continuation that actually added
// text out of the diminishing-returns counter.
func TestLargeContinuationsDoNotTrip(t *testing.T) {
	br := newBreaker()
	big := strings.Repeat("x", 2100)
	for c := 1; c <= 5; c++ {
		remind, stop, reason := br.BeginContinuation()
		if stop || remind != "" {
			t.Fatalf("begin %d remind=%q stop=%v %s", c, remind, stop, reason)
		}
		id := string(rune('a' + c))
		if _, stop, reason := br.Observe([]provider.Message{
			assistantCall(id, id+" "+big, "echo", `{"n":"`+id+`"}`),
			toolResult(id, "echo", "ok"),
		}); stop {
			t.Fatalf("observe %d stopped (%s)", c, reason)
		}
	}
}

func shellCall(id, command, output string) []provider.Message {
	args := `{"command":` + jsonString(command) + `}`
	return []provider.Message{
		assistantCall(id, "run "+id+" "+command, "shell", args),
		toolResult(id, "shell", output),
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestEditAndRetestDoesNotStop is the edit + `go test ./...` loop. Failing
// test output does not start with "error:", so it used to count as a successful
// repeat and hard-stop on the fourth test.
func TestEditAndRetestDoesNotStop(t *testing.T) {
	br := newBreaker()
	fail := "FAIL\txbot/internal/kernel [build failed]\n"
	for i := 1; i <= 4; i++ {
		id := string(rune('a' + i))
		edit := []provider.Message{
			assistantCall("e"+id, "edit "+id, "edit_file", `{"path":"a.go","old_string":"old`+id+`","new_string":"new`+id+`"}`),
			toolResult("e"+id, "edit_file", "ok"),
		}
		for _, step := range [][]provider.Message{edit, shellCall("t"+id, "go test ./...", fail)} {
			remind, stop, reason := br.Observe(step)
			if stop || strings.Contains(remind, StopSameToolArgs) {
				t.Fatalf("cycle %s remind=%q stop=%v reason=%q", id, remind, stop, reason)
			}
		}
	}
}

// TestBuildAndVetDoNotShareAStop runs `go build` and `go vet` three times each.
// Those used to share one same_tool_args trip counter and hard-stop on call 6.
func TestBuildAndVetDoNotShareAStop(t *testing.T) {
	br := newBreaker()
	var n int
	run := func(command string) {
		t.Helper()
		for i := 0; i < 3; i++ {
			n++
			id := string(rune('a' + n))
			remind, stop, reason := br.Observe(shellCall(id, command, "ok"))
			if stop || strings.Contains(remind, StopSameToolArgs) {
				t.Fatalf("%s #%d remind=%q stop=%v reason=%q", command, i+1, remind, stop, reason)
			}
		}
	}
	run("go build ./...")
	run("go vet ./...")
}

// TestSuccessResetsPollingFailure is fail, fail, ok, fail. The success must
// clear the failure streak so the last failure does not trip polling_failure.
func TestSuccessResetsPollingFailure(t *testing.T) {
	br := newBreaker()
	steps := []struct {
		id, out string
	}{
		{"1", "error: connection refused"},
		{"2", "error: connection refused"},
		{"3", "ok"},
		{"4", "error: connection refused"},
	}
	for _, s := range steps {
		remind, stop, reason := br.Observe([]provider.Message{
			assistantCall(s.id, "try "+s.id, "http_request", `{"url":"http://down"}`),
			toolResult(s.id, "http_request", s.out),
		})
		if stop || strings.Contains(remind, StopPollingFailure) {
			t.Fatalf("step %s remind=%q stop=%v reason=%q", s.id, remind, stop, reason)
		}
	}
}

func BenchmarkBreakerObserve(b *testing.B) {
	step := []provider.Message{
		assistantCall("1", "hello", "echo", `{"n":"1"}`),
		toolResult("1", "echo", "ok"),
	}
	br := newBreaker()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		br.Observe(step)
	}
}
