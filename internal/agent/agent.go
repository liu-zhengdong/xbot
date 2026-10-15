// Package agent runs the model/tool loop. It does not know about bots, links, or memory.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

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
	// OnTool and OnResult may run at the same time, once per call.
	// Callers that stream them (the terminal printer, the web SSE writer) must tolerate that.
	OnTool    func(name, summary string)
	OnResult  func(name, output string)
	OnMessage func(provider.Message)
	// AfterStep runs after a step's tool results and before the next model call.
	// Messages it returns are appended in order and shown to the next step.
	AfterStep func() ([]provider.Message, error)
	// OneRound returns after a single model call and that call's tool results.
	// The step limit error is not used. The caller decides what happens next.
	OneRound bool
}

// Run calls the model until it stops requesting tools or the step limit is hit.
// Tool calls in one response run concurrently and each respects ctx cancellation.
// Approvals are asked one at a time; calls that do not need approval keep going.
// Results are appended in the original call order so the transcript stays deterministic.
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
		for _, tm := range runToolCalls(ctx, byName, msg.ToolCalls, opt) {
			convo = append(convo, tm)
			produced = append(produced, tm)
			if opt.OnMessage != nil {
				opt.OnMessage(tm)
			}
		}
		if opt.OneRound {
			return produced, nil
		}
		if opt.AfterStep != nil {
			extra, err := opt.AfterStep()
			if err != nil {
				return produced, err
			}
			for _, m := range extra {
				convo = append(convo, m)
				produced = append(produced, m)
				if opt.OnMessage != nil {
					opt.OnMessage(m)
				}
			}
		}
	}
	return stopSummary(ctx, p, convo, produced, opt)
}

// stopSummary asks for a status with tools disabled after the step cap.
// The turn ends with that assistant text. Tool calls in the reply are dropped.
// An empty reply is replaced so the owner still sees why the turn stopped.
func stopSummary(ctx context.Context, p provider.Provider, convo, produced []provider.Message, opt Options) ([]provider.Message, error) {
	if err := ctx.Err(); err != nil {
		return produced, err
	}
	note := fmt.Sprintf("Stop. The step cap (%d) is reached. Do not call tools. Summarize what was done, what is still pending, and why you stopped.", opt.MaxSteps)
	convo = append(convo, provider.Message{Role: provider.RoleUser, Content: note})
	msg, err := p.Chat(ctx, provider.Request{Messages: convo}, opt.OnDelta)
	if err != nil {
		return produced, fmt.Errorf("agent: max steps (%d) reached: %w", opt.MaxSteps, err)
	}
	if msg.Role == "" {
		msg.Role = provider.RoleAssistant
	}
	msg.ToolCalls = nil
	if strings.TrimSpace(msg.Content) == "" {
		msg.Content = fmt.Sprintf("Stopped after %d steps, before the task finished. The last response still requested tools, so they were not run.", opt.MaxSteps)
		if opt.OnDelta != nil {
			opt.OnDelta(msg.Content)
		}
	}
	produced = append(produced, msg)
	if opt.OnMessage != nil {
		opt.OnMessage(msg)
	}
	return produced, nil
}

// runToolCalls starts every call, then returns tool messages in call order.
// The approval lock is held only around Approve, so a prompt does not stall the others.
func runToolCalls(ctx context.Context, byName map[string]tools.Tool, calls []provider.ToolCall, opt Options) []provider.Message {
	out := make([]string, len(calls))
	var approve sync.Mutex
	var wg sync.WaitGroup
	wg.Add(len(calls))
	for i, tc := range calls {
		go func(i int, tc provider.ToolCall) {
			defer wg.Done()
			out[i] = execTool(ctx, byName, tc, opt, &approve)
		}(i, tc)
	}
	wg.Wait()
	msgs := make([]provider.Message, len(calls))
	for i, tc := range calls {
		msgs[i] = provider.Message{
			Role:       provider.RoleTool,
			ToolCallID: tc.ID,
			Name:       tc.Function.Name,
			Content:    out[i],
		}
	}
	return msgs
}

func execTool(ctx context.Context, byName map[string]tools.Tool, tc provider.ToolCall, opt Options, approve *sync.Mutex) string {
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
		if opt.Approve == nil {
			return finish(opt, t.Name(), "not run: no approver available on this link")
		}
		approve.Lock()
		approved, err := opt.Approve(ctx, t.Name(), summary)
		approve.Unlock()
		if err != nil {
			return finish(opt, t.Name(), "error: "+err.Error())
		}
		if !approved {
			return finish(opt, t.Name(), "denied by user")
		}
	}
	if err := ctx.Err(); err != nil {
		return finish(opt, t.Name(), "error: "+err.Error())
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
