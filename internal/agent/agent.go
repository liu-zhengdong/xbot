// Package agent runs the model/tool loop. It does not know about bots, links, or memory.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"xbot/internal/clip"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

const defaultMaxSteps = 8
const defaultMaxResult = 64 << 10

// Approver decides whether a confirmed tool call may run.
type Approver func(ctx context.Context, toolName, summary string) (bool, error)

// Options controls one agent run.
type Options struct {
	MaxSteps  int
	MaxResult int
	Approve   Approver
	OnDelta   func(string)
	OnTool    func(name, summary string)
	OnResult  func(name, output string)
	OnMessage func(provider.Message)
	// OneRound returns after a single model call and that call's tool results.
	// The step limit error is not used. The caller decides what happens next.
	OneRound bool
}

// Run calls the model until it stops requesting tools or the step limit is hit.
// Tool calls in one response run one at a time, in order.
// The returned messages are only the ones produced in this run.
func Run(ctx context.Context, p provider.Provider, list []tools.Tool, messages []provider.Message, opt Options) ([]provider.Message, error) {
	if opt.MaxSteps <= 0 {
		opt.MaxSteps = defaultMaxSteps
	}
	if opt.MaxResult <= 0 {
		opt.MaxResult = defaultMaxResult
	}
	byName := make(map[string]tools.Tool, len(list))
	defs := make([]provider.ToolDef, 0, len(list))
	for _, t := range list {
		if _, ok := byName[t.Name()]; ok {
			return nil, fmt.Errorf("agent: duplicate tool %q", t.Name())
		}
		byName[t.Name()] = t
		defs = append(defs, provider.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}
	convo := append([]provider.Message(nil), messages...)
	var produced []provider.Message
	for step := 0; step < opt.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return produced, err
		}
		msg, err := p.Chat(ctx, provider.Request{Messages: convo, Tools: defs}, opt.OnDelta)
		if err != nil {
			return produced, err
		}
		if msg.Role == "" {
			msg.Role = provider.RoleAssistant
		}
		convo = append(convo, msg)
		produced = append(produced, msg)
		if opt.OnMessage != nil {
			opt.OnMessage(msg)
		}
		if len(msg.ToolCalls) == 0 {
			return produced, nil
		}
		for _, tc := range msg.ToolCalls {
			result := execTool(ctx, byName, tc, opt)
			tm := provider.Message{
				Role:       provider.RoleTool,
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
				Content:    result,
			}
			convo = append(convo, tm)
			produced = append(produced, tm)
			if opt.OnMessage != nil {
				opt.OnMessage(tm)
			}
		}
		if opt.OneRound {
			return produced, nil
		}
	}
	return produced, fmt.Errorf("agent: max steps (%d) reached", opt.MaxSteps)
}

func execTool(ctx context.Context, byName map[string]tools.Tool, tc provider.ToolCall, opt Options) string {
	t, ok := byName[tc.Function.Name]
	if !ok {
		return finish(opt, tc.Function.Name, fmt.Sprintf("error: unknown tool %q", tc.Function.Name))
	}
	raw := json.RawMessage(tc.Function.Arguments)
	if !json.Valid(raw) {
		return finish(opt, t.Name(), "error: tool arguments are not valid JSON")
	}
	need, summary, err := t.Confirm(raw)
	if err != nil {
		return finish(opt, t.Name(), "error: "+err.Error())
	}
	if opt.OnTool != nil {
		opt.OnTool(t.Name(), summary)
	}
	if need {
		approved := false
		if opt.Approve != nil {
			approved, err = opt.Approve(ctx, t.Name(), summary)
			if err != nil {
				return finish(opt, t.Name(), "error: "+err.Error())
			}
		}
		if !approved {
			return finish(opt, t.Name(), "denied by user")
		}
	}
	out, err := t.Run(ctx, raw)
	if err != nil {
		out = "error: " + err.Error()
	}
	if strings.TrimSpace(out) == "" {
		out = "(empty)"
	}
	out, _ = clip.Bytes(out, opt.MaxResult)
	return finish(opt, t.Name(), out)
}

func finish(opt Options, name, out string) string {
	if opt.OnResult != nil {
		opt.OnResult(name, out)
	}
	return out
}
