package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

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
	var order []string
	a := &seqTool{name: "a", order: &order}
	b := &seqTool{name: "b", order: &order}
	p := &scripted{resps: []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
			{ID: "1", Type: "function", Function: provider.FunctionCall{Name: "a", Arguments: "{}"}},
			{ID: "2", Type: "function", Function: provider.FunctionCall{Name: "b", Arguments: "{}"}},
		}},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	if _, err := Run(context.Background(), p, []tools.Tool{a, b}, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(order, ",") != "a,b" {
		t.Fatalf("order %v", order)
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

type seqTool struct {
	name  string
	order *[]string
}

func (s *seqTool) Name() string        { return s.name }
func (s *seqTool) Description() string { return s.name }
func (s *seqTool) Parameters() any     { return map[string]any{"type": "object"} }
func (s *seqTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, s.name, nil
}
func (s *seqTool) Run(context.Context, json.RawMessage) (string, error) {
	*s.order = append(*s.order, s.name)
	return s.name, nil
}
