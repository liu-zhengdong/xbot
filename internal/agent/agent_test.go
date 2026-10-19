package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/provider"
	"xbot/internal/tools"
)

type scripted struct {
	resps []provider.Message
	reqs  []provider.Request
	i     int
}

func snapshotReq(req provider.Request) provider.Request {
	msgs := make([]provider.Message, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = m
		if len(m.Images) > 0 {
			msgs[i].Images = append([]provider.Image(nil), m.Images...)
		}
	}
	req.Messages = msgs
	return req
}

func (s *scripted) Chat(_ context.Context, req provider.Request, onDelta func(string)) (provider.Message, error) {
	s.reqs = append(s.reqs, snapshotReq(req))
	if s.i >= len(s.resps) {
		return provider.Message{}, errors.New("no more responses")
	}
	m := s.resps[s.i]
	s.i++
	if onDelta != nil && m.Content != "" {
		onDelta(m.Content)
	}
	return m, nil
}

type echoTool struct {
	confirm bool
	ran     bool
}

func (e *echoTool) Name() string        { return "echo" }
func (e *echoTool) Description() string { return "echo" }
func (e *echoTool) Parameters() any     { return map[string]any{"type": "object"} }
func (e *echoTool) Confirm(json.RawMessage) (bool, string, error) {
	return e.confirm, "echo", nil
}
func (e *echoTool) Run(context.Context, json.RawMessage) (string, error) {
	e.ran = true
	return "echoed", nil
}

func TestRunFinalAnswer(t *testing.T) {
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: "Hello"}}}
	var deltas []string
	got, err := Run(context.Background(), p, nil, []provider.Message{{Role: provider.RoleUser, Content: "hi"}}, Options{
		OnDelta: func(s string) { deltas = append(deltas, s) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Content != "Hello" {
		t.Fatalf("%+v", got)
	}
	if strings.Join(deltas, "") != "Hello" {
		t.Fatalf("deltas %v", deltas)
	}
}

func TestRunToolThenAnswer(t *testing.T) {
	tool := &echoTool{}
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "echo", Arguments: "{}"},
		}}},
		{Role: provider.RoleAssistant, Content: "done"},
	}}
	got, err := Run(context.Background(), p, []tools.Tool{tool}, []provider.Message{{Role: provider.RoleUser, Content: "go"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !tool.ran {
		t.Fatal("tool did not run")
	}
	if len(got) != 3 || got[1].Role != provider.RoleTool || got[1].Content != "echoed" || got[2].Content != "done" {
		t.Fatalf("%+v", got)
	}
	if len(p.reqs) != 2 {
		t.Fatalf("calls %d", len(p.reqs))
	}
	if p.reqs[1].Messages[len(p.reqs[1].Messages)-1].Content != "echoed" {
		t.Fatal("tool result was not sent back")
	}
}

func TestRunDeniedSkipsTool(t *testing.T) {
	tool := &echoTool{confirm: true}
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "echo", Arguments: "{}"},
		}}},
		{Role: provider.RoleAssistant, Content: "stopped"},
	}}
	got, err := Run(context.Background(), p, []tools.Tool{tool}, nil, Options{
		Approve: func(context.Context, string, string) (bool, error) { return false, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if tool.ran {
		t.Fatal("tool ran after denial")
	}
	if got[1].Content != "denied by user" || got[2].Content != "stopped" {
		t.Fatalf("%+v", got)
	}
}

func TestRunWithoutApproverDoesNotBlameTheUser(t *testing.T) {
	tool := &echoTool{confirm: true}
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "echo", Arguments: "{}"},
		}}},
		{Role: provider.RoleAssistant, Content: "stopped"},
	}}
	got, err := Run(context.Background(), p, []tools.Tool{tool}, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if tool.ran {
		t.Fatal("tool ran without an approver")
	}
	if strings.Contains(got[1].Content, "user") || !strings.Contains(got[1].Content, "no approver") {
		t.Fatalf("%+v", got)
	}
}

func TestRunToolErrorContinues(t *testing.T) {
	tool := &failTool{}
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "fail", Arguments: "{}"},
		}}},
		{Role: provider.RoleAssistant, Content: "recovered"},
	}}
	got, err := Run(context.Background(), p, []tools.Tool{tool}, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got[1].Content, "error:") || got[2].Content != "recovered" {
		t.Fatalf("%+v", got)
	}
}

func TestRunUnknownTool(t *testing.T) {
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "nope", Arguments: "{}"},
		}}},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	got, err := Run(context.Background(), p, nil, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got[1].Content, "unknown tool") {
		t.Fatalf("%+v", got)
	}
}

type alwaysTools struct {
	reqs []provider.Request
}

func (a *alwaysTools) Chat(_ context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	a.reqs = append(a.reqs, req)
	return provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
		ID: "c", Type: "function", Function: provider.FunctionCall{Name: "echo", Arguments: "{}"},
	}}}, nil
}

func TestRunMaxSteps(t *testing.T) {
	p := &alwaysTools{}
	got, err := Run(context.Background(), p, []tools.Tool{&echoTool{}}, nil, Options{MaxSteps: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) != 3 || len(p.reqs[2].Tools) != 0 {
		t.Fatalf("summary request %+v", p.reqs)
	}
	last := got[len(got)-1]
	if last.Role != provider.RoleAssistant || strings.TrimSpace(last.Content) == "" || len(last.ToolCalls) != 0 {
		t.Fatalf("final %+v", last)
	}
}

func TestRunDuplicateTool(t *testing.T) {
	_, err := Run(context.Background(), &scripted{}, []tools.Tool{&echoTool{}, &echoTool{}}, nil, Options{})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatal(err)
	}
}

func TestRunToolsInOrder(t *testing.T) {
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
			{ID: "1", Type: "function", Function: provider.FunctionCall{Name: "a", Arguments: "{}"}},
			{ID: "2", Type: "function", Function: provider.FunctionCall{Name: "b", Arguments: "{}"}},
		}},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	got, err := Run(context.Background(), p, []tools.Tool{&sleepTool{name: "a"}, &sleepTool{name: "b"}}, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got[1].ToolCallID != "1" || got[1].Content != "a" || got[2].ToolCallID != "2" || got[2].Content != "b" {
		t.Fatalf("%+v", got)
	}
}

type failTool struct{}

func (failTool) Name() string        { return "fail" }
func (failTool) Description() string { return "fail" }
func (failTool) Parameters() any     { return map[string]any{"type": "object"} }
func (failTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "fail", nil
}
func (failTool) Run(context.Context, json.RawMessage) (string, error) {
	return "", errors.New("boom")
}

type sleepTool struct {
	name    string
	d       time.Duration
	confirm bool
	entered chan struct{}
	onDone  func(string)
}

func (s *sleepTool) Name() string        { return s.name }
func (s *sleepTool) Description() string { return s.name }
func (s *sleepTool) Parameters() any     { return map[string]any{"type": "object"} }
func (s *sleepTool) Confirm(json.RawMessage) (bool, string, error) {
	return s.confirm, s.name, nil
}
func (s *sleepTool) Run(ctx context.Context, _ json.RawMessage) (string, error) {
	if s.entered != nil {
		s.entered <- struct{}{}
	}
	timer := time.NewTimer(s.d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
		if s.onDone != nil {
			s.onDone(s.name)
		}
		return s.name, nil
	}
}

func TestParallelToolsBeatSerialTime(t *testing.T) {
	const d = 200 * time.Millisecond
	p := &scripted{resps: []provider.Message{{
		Role: provider.RoleAssistant,
		ToolCalls: []provider.ToolCall{
			{ID: "1", Type: "function", Function: provider.FunctionCall{Name: "a", Arguments: "{}"}},
			{ID: "2", Type: "function", Function: provider.FunctionCall{Name: "b", Arguments: "{}"}},
			{ID: "3", Type: "function", Function: provider.FunctionCall{Name: "c", Arguments: "{}"}},
		},
	}}}
	start := time.Now()
	got, err := Run(context.Background(), p, []tools.Tool{
		&sleepTool{name: "a", d: d},
		&sleepTool{name: "b", d: d},
		&sleepTool{name: "c", d: d},
	}, nil, Options{OneRound: true})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed >= 2*d {
		t.Fatalf("parallel took %s; serial would be %s", elapsed, 3*d)
	}
	if got[1].Content != "a" || got[2].Content != "b" || got[3].Content != "c" {
		t.Fatalf("order %+v", got)
	}
}

func TestToolResultsStayInCallOrder(t *testing.T) {
	var mu sync.Mutex
	var finished []string
	mark := func(name string) {
		mu.Lock()
		finished = append(finished, name)
		mu.Unlock()
	}
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
			{ID: "1", Type: "function", Function: provider.FunctionCall{Name: "slow", Arguments: "{}"}},
			{ID: "2", Type: "function", Function: provider.FunctionCall{Name: "fast", Arguments: "{}"}},
		}},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	got, err := Run(context.Background(), p, []tools.Tool{
		&sleepTool{name: "slow", d: 150 * time.Millisecond, onDone: mark},
		&sleepTool{name: "fast", onDone: mark},
	}, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(finished, ",") != "fast,slow" {
		t.Fatalf("finish order %v", finished)
	}
	if got[1].ToolCallID != "1" || got[1].Content != "slow" || got[2].ToolCallID != "2" || got[2].Content != "fast" {
		t.Fatalf("transcript %+v", got[1:3])
	}
	msgs := p.reqs[1].Messages
	if msgs[len(msgs)-2].Content != "slow" || msgs[len(msgs)-1].Content != "fast" {
		t.Fatalf("next request %+v", msgs[len(msgs)-2:])
	}
}

func TestApprovalsAreSerialized(t *testing.T) {
	var mu sync.Mutex
	var active, maxActive int
	started := make(chan struct{})
	var once sync.Once
	free := &overlapTool{
		started: started,
		active: func() int {
			mu.Lock()
			defer mu.Unlock()
			return active
		},
	}
	p := &scripted{resps: []provider.Message{{
		Role: provider.RoleAssistant,
		ToolCalls: []provider.ToolCall{
			{ID: "1", Type: "function", Function: provider.FunctionCall{Name: "ask-a", Arguments: "{}"}},
			{ID: "2", Type: "function", Function: provider.FunctionCall{Name: "free", Arguments: "{}"}},
			{ID: "3", Type: "function", Function: provider.FunctionCall{Name: "ask-b", Arguments: "{}"}},
		},
	}}}
	got, err := Run(context.Background(), p, []tools.Tool{
		&sleepTool{name: "ask-a", confirm: true},
		free,
		&sleepTool{name: "ask-b", confirm: true},
	}, nil, Options{
		OneRound: true,
		Approve: func(context.Context, string, string) (bool, error) {
			once.Do(func() { close(started) })
			mu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			mu.Unlock()
			time.Sleep(120 * time.Millisecond)
			mu.Lock()
			active--
			mu.Unlock()
			return true, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if maxActive != 1 {
		t.Fatalf("approvals in flight %d", maxActive)
	}
	if !free.saw || got[2].Content != "free" {
		t.Fatalf("free tool %+v saw %v", got[2], free.saw)
	}
	if got[1].ToolCallID != "1" || got[3].ToolCallID != "3" {
		t.Fatalf("order %+v", got)
	}
}

type overlapTool struct {
	started <-chan struct{}
	active  func() int
	saw     bool
}

func (overlapTool) Name() string        { return "free" }
func (overlapTool) Description() string { return "free" }
func (overlapTool) Parameters() any     { return map[string]any{"type": "object"} }
func (overlapTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "free", nil
}
func (o *overlapTool) Run(ctx context.Context, _ json.RawMessage) (string, error) {
	select {
	case <-o.started:
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(time.Second):
		return "", errors.New("approval did not start")
	}
	if o.active() < 1 {
		return "", errors.New("approval was not in progress")
	}
	o.saw = true
	return "free", nil
}

func TestCancelStopsInFlightTools(t *testing.T) {
	entered := make(chan struct{}, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-entered
		<-entered
		cancel()
	}()
	p := &scripted{resps: []provider.Message{{
		Role: provider.RoleAssistant,
		ToolCalls: []provider.ToolCall{
			{ID: "1", Type: "function", Function: provider.FunctionCall{Name: "a", Arguments: "{}"}},
			{ID: "2", Type: "function", Function: provider.FunctionCall{Name: "b", Arguments: "{}"}},
		},
	}}}
	start := time.Now()
	got, err := Run(ctx, p, []tools.Tool{
		&sleepTool{name: "a", d: 5 * time.Second, entered: entered},
		&sleepTool{name: "b", d: 5 * time.Second, entered: entered},
	}, nil, Options{})
	elapsed := time.Since(start)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if elapsed > time.Second {
		t.Fatalf("cancel took %s", elapsed)
	}
	// Cancelled calls stay open so the kernel can seal them once. The assistant
	// request is kept; the fake "error: context canceled" results are not.
	if len(got) != 1 || len(got[0].ToolCalls) != 2 {
		t.Fatalf("%+v", got)
	}
}

func TestGuardRemindsThenStops(t *testing.T) {
	tool := &echoTool{}
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "echo", Arguments: `{"n":"1"}`},
		}}},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c2", Type: "function", Function: provider.FunctionCall{Name: "echo", Arguments: `{"n":"2"}`},
		}}},
		{Role: provider.RoleAssistant, Content: "summarized"},
	}}
	var stops []string
	got, err := Run(context.Background(), p, []tools.Tool{tool}, []provider.Message{
		{Role: provider.RoleUser, Content: "go"},
	}, Options{
		Guard: func(step int, _ []provider.Message) (string, bool, string) {
			if step == 0 {
				return "loop warning", false, ""
			}
			return "", true, "repeated_output"
		},
		OnStop: func(reason string) { stops = append(stops, reason) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(stops) != 1 || stops[0] != "repeated_output" {
		t.Fatalf("stops %v", stops)
	}
	if len(p.reqs) != 3 {
		t.Fatalf("requests %d", len(p.reqs))
	}
	last := p.reqs[1].Messages[len(p.reqs[1].Messages)-1]
	if !strings.Contains(last.Content, "loop warning") {
		t.Fatalf("reminder not on the newest message: %q", last.Content)
	}
	if strings.Contains(p.reqs[1].Messages[0].Content, "loop warning") {
		t.Fatal("reminder rewrote an earlier message")
	}
	var sawReason, sawSummary bool
	for _, m := range got {
		if strings.Contains(m.Content, "<stop_reason>repeated_output</stop_reason>") {
			sawReason = true
		}
		if m.Content == "summarized" {
			sawSummary = true
		}
	}
	if !sawReason || !sawSummary {
		t.Fatalf("produced %+v", got)
	}
}

type panicTool struct{}

func (panicTool) Name() string                                  { return "boom" }
func (panicTool) Description() string                           { return "boom" }
func (panicTool) Parameters() any                               { return map[string]any{"type": "object"} }
func (panicTool) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }
func (panicTool) Run(context.Context, json.RawMessage) (string, error) {
	panic("hostile page")
}

func TestToolPanicBecomesError(t *testing.T) {
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "boom", Arguments: "{}"},
		}}},
		{Role: provider.RoleAssistant, Content: "still here"},
	}}
	got, err := Run(context.Background(), p, []tools.Tool{panicTool{}}, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 || !strings.Contains(got[1].Content, "error: tool panic: hostile page") {
		t.Fatalf("%+v", got)
	}
}

type imageTool struct {
	n    int
	size int
}

func (imageTool) Name() string                                  { return "view_image" }
func (imageTool) Description() string                           { return "view" }
func (imageTool) Parameters() any                               { return map[string]any{"type": "object"} }
func (imageTool) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }
func (im imageTool) Run(ctx context.Context, _ json.RawMessage) (string, error) {
	n := im.n
	if n <= 0 {
		n = 1
	}
	size := im.size
	if size <= 0 {
		size = 4
	}
	for i := 0; i < n; i++ {
		tools.AttachImage(ctx, tools.AttachedImage{MIME: "image/png", Data: bytes.Repeat([]byte{byte(i + 1)}, size)})
	}
	return "path: a.png", nil
}

func TestImageStaysUntilTheTurnEnds(t *testing.T) {
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "view_image", Arguments: "{}"},
		}}},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c2", Type: "function", Function: provider.FunctionCall{Name: "echo", Arguments: "{}"},
		}}},
		{Role: provider.RoleAssistant, Content: "done"},
	}}
	_, err := Run(context.Background(), p, []tools.Tool{imageTool{}, &echoTool{}}, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) < 3 {
		t.Fatalf("requests %d", len(p.reqs))
	}
	for i, req := range p.reqs[1:] {
		var n int
		for _, m := range req.Messages {
			n += len(m.Images)
			if strings.Contains(m.Content, "[image omitted after the previous step]") {
				t.Fatalf("request %d told the model the image was gone: %q", i+1, m.Content)
			}
		}
		if n != 1 {
			t.Fatalf("request %d carried %d images", i+1, n)
		}
	}
}

type seqImage struct{ n int }

func (seqImage) Name() string                                  { return "view_image" }
func (seqImage) Description() string                           { return "view" }
func (seqImage) Parameters() any                               { return map[string]any{"type": "object"} }
func (seqImage) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }
func (im *seqImage) Run(ctx context.Context, _ json.RawMessage) (string, error) {
	im.n++
	tools.AttachImage(ctx, tools.AttachedImage{MIME: "image/png", Data: []byte{byte(im.n)}})
	return "path: a.png", nil
}

func TestNewestImageSetReplacesOlder(t *testing.T) {
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "view_image", Arguments: "{}"},
		}}},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c2", Type: "function", Function: provider.FunctionCall{Name: "view_image", Arguments: "{}"},
		}}},
		{Role: provider.RoleAssistant, Content: "done"},
	}}
	_, err := Run(context.Background(), p, []tools.Tool{&seqImage{}}, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) < 3 {
		t.Fatalf("requests %d", len(p.reqs))
	}
	var marks []byte
	for _, m := range p.reqs[2].Messages {
		for _, img := range m.Images {
			marks = append(marks, img.Data...)
		}
	}
	if len(marks) != 1 || marks[0] != 2 {
		t.Fatalf("final request images %v", marks)
	}
}

func TestImageTurnCap(t *testing.T) {
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "view_image", Arguments: "{}"},
		}}},
		{Role: provider.RoleAssistant, Content: "done"},
	}}
	_, err := Run(context.Background(), p, []tools.Tool{imageTool{n: 5, size: 8}}, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	var sawNote bool
	for _, m := range p.reqs[1].Messages {
		n += len(m.Images)
		if strings.Contains(m.Content, "[image omitted: turn image cap]") {
			sawNote = true
		}
	}
	if n != 4 || !sawNote {
		t.Fatalf("images %d note %v", n, sawNote)
	}
}

type approvePanicTool struct{}

func (approvePanicTool) Name() string                                  { return "needs" }
func (approvePanicTool) Description() string                           { return "needs" }
func (approvePanicTool) Parameters() any                               { return map[string]any{"type": "object"} }
func (approvePanicTool) Confirm(json.RawMessage) (bool, string, error) { return true, "do it", nil }
func (approvePanicTool) Run(context.Context, json.RawMessage) (string, error) {
	return "ok", nil
}

func TestApprovePanicDoesNotDeadlock(t *testing.T) {
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
			{ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "needs", Arguments: "{}"}},
			{ID: "c2", Type: "function", Function: provider.FunctionCall{Name: "needs", Arguments: "{}"}},
		}},
		{Role: provider.RoleAssistant, Content: "done"},
	}}
	done := make(chan struct{})
	var got []provider.Message
	var err error
	go func() {
		defer close(done)
		got, err = Run(context.Background(), p, []tools.Tool{approvePanicTool{}}, nil, Options{
			Approve: func(context.Context, string, string) (bool, error) {
				panic("approve blew up")
			},
		})
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("approve panic held the lock")
	}
	if err != nil {
		t.Fatal(err)
	}
	var panics int
	for _, m := range got {
		if strings.Contains(m.Content, "error: tool panic:") {
			panics++
		}
	}
	if panics != 2 {
		t.Fatalf("panics %d in %+v", panics, got)
	}
}

func TestToolPanicIsRedactedAndClipped(t *testing.T) {
	secret := "sk-secret-value"
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "boom", Arguments: "{}"},
		}}},
		{Role: provider.RoleAssistant, Content: "still here"},
	}}
	tool := panicTextTool{text: secret + " " + strings.Repeat("x", 8000)}
	got, err := Run(context.Background(), p, []tools.Tool{tool}, nil, Options{
		MaxResult: 128,
		Redact: func(s string) string {
			return strings.ReplaceAll(s, secret, "[secret]")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 || strings.Contains(got[1].Content, secret) || !strings.Contains(got[1].Content, "[secret]") {
		t.Fatalf("%+v", got)
	}
	if len(got[1].Content) > 128 {
		t.Fatalf("panic text len %d", len(got[1].Content))
	}
}

type panicTextTool struct{ text string }

func (panicTextTool) Name() string                                  { return "boom" }
func (panicTextTool) Description() string                           { return "boom" }
func (panicTextTool) Parameters() any                               { return map[string]any{"type": "object"} }
func (panicTextTool) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }
func (p panicTextTool) Run(context.Context, json.RawMessage) (string, error) {
	panic(p.text)
}

// TestAbsorbedPicturesCountAgainstTheTurnCap is the regression for a message
// absorbed mid-turn: its pictures arrive through AfterStep, not through a tool,
// so the budget used to miss them. A tool's four pictures plus an absorbed
// fifth must not reach the next request as five.
func TestAbsorbedPicturesCountAgainstTheTurnCap(t *testing.T) {
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "view_image", Arguments: "{}"},
		}}},
		{Role: provider.RoleAssistant, Content: "done"},
	}}
	var absorbed bool
	opt := Options{AfterStep: func() ([]provider.Message, error) {
		if absorbed {
			return nil, nil
		}
		absorbed = true
		return []provider.Message{{
			Role:    provider.RoleUser,
			Content: "看这张",
			Images:  []provider.Image{{MIME: "image/png", Data: []byte{9}}},
		}}, nil
	}}
	// Four pictures from the tool use up the turn's allowance.
	_, err := Run(context.Background(), p, []tools.Tool{imageTool{n: 4, size: 8}}, nil, opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) < 2 {
		t.Fatalf("requests %d", len(p.reqs))
	}
	var n int
	for _, m := range p.reqs[len(p.reqs)-1].Messages {
		n += len(m.Images)
	}
	if n > maxTurnImages {
		t.Fatalf("the last request carried %d pictures, over the cap of %d", n, maxTurnImages)
	}
}
