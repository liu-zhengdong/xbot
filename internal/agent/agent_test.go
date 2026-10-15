package agent

import (
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

func (s *scripted) Chat(_ context.Context, req provider.Request, onDelta func(string)) (provider.Message, error) {
	s.reqs = append(s.reqs, req)
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

func TestRunMaxSteps(t *testing.T) {
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "echo", Arguments: "{}"},
		}}},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c2", Type: "function", Function: provider.FunctionCall{Name: "echo", Arguments: "{}"},
		}}},
	}}
	_, err := Run(context.Background(), p, []tools.Tool{&echoTool{}}, nil, Options{MaxSteps: 2})
	if err == nil || !strings.Contains(err.Error(), "max steps") {
		t.Fatalf("err=%v", err)
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
	if len(got) != 3 || !strings.Contains(got[1].Content, "canceled") || !strings.Contains(got[2].Content, "canceled") {
		t.Fatalf("%+v", got)
	}
	if got[1].ToolCallID != "1" || got[2].ToolCallID != "2" {
		t.Fatalf("order %+v", got[1:])
	}
}
