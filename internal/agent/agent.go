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

type toolArgsKey struct{}

// withToolArgs carries one tool call's raw arguments to the approver without
// widening the Approver signature.
func withToolArgs(ctx context.Context, args json.RawMessage) context.Context {
	return context.WithValue(ctx, toolArgsKey{}, args)
}

// ToolArgs returns the raw arguments of the tool call the approver is deciding
// on, or nil when there are none. The web approval card uses it to label the
// action.
func ToolArgs(ctx context.Context) json.RawMessage {
	args, _ := ctx.Value(toolArgsKey{}).(json.RawMessage)
	return args
}

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
	// Redact masks known secret values in tool output before it is streamed or
	// recorded. Nil leaves the output unchanged.
	Redact func(string) string
	// AfterStep runs after a step's tool results and before the next model call.
	// Messages it returns are appended in order and shown to the next step.
	AfterStep func() ([]provider.Message, error)
	// Guard inspects one model step (the assistant message and its tool
	// results) after AfterStep. remind is appended to the newest message.
	// stop ends the run: the reason is recorded and the model is asked to
	// summarize with tools disabled. Nil skips the check.
	Guard func(step int, msgs []provider.Message) (remind string, stop bool, reason string)
	// OnReminder is called with the reminder text Guard returned, after it has
	// been attached to the in-memory conversation.
	OnReminder func(text string)
	// OnStop is called when Guard stops the run, before the summary request.
	OnStop func(reason string)
	// RefreshTools rebuilds the tool list before each model call after the first.
	// Nil keeps the list from the start of the run.
	RefreshTools func() ([]tools.Tool, error)
	// OneRound returns after a single model call and that call's tool results.
	// The step limit error is not used. The caller decides what happens next.
	OneRound bool
	// OnCap runs when the run ends because it used all MaxSteps, before the
	// closing summary is asked for. A nil OnCap leaves the reason invisible to
	// the caller. The run returns nil error either way.
	OnCap func()
	// OnStep runs once per model call, before the request goes out. A caller
	// that caps a task across several runs counts steps with it.
	OnStep func()
	// OnIdle runs when the model stops without tool calls and another step
	// still fits in MaxSteps. cont asks for one more step. remind is sent as
	// its own user message, not appended to the assistant line. The caller
	// must not continue forever.
	OnIdle func(msg provider.Message) (remind string, cont bool)
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
	byName, defs, err := indexTools(list)
	if err != nil {
		return nil, err
	}
	convo := append([]provider.Message(nil), messages...)
	var produced []provider.Message
	for step := 0; step < opt.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return produced, err
		}
		if step > 0 && opt.RefreshTools != nil {
			next, err := opt.RefreshTools()
			if err != nil {
				return produced, err
			}
			byName, defs, err = indexTools(next)
			if err != nil {
				return produced, err
			}
		}
		if opt.OnStep != nil {
			opt.OnStep()
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
			if opt.OnIdle != nil && step+1 < opt.MaxSteps {
				remind, cont := opt.OnIdle(msg)
				if cont {
					if remind != "" {
						note := provider.Message{Role: provider.RoleUser, Content: strings.TrimSpace(remind)}
						convo = append(convo, note)
						produced = append(produced, note)
						if opt.OnMessage != nil {
							opt.OnMessage(note)
						}
						if opt.OnReminder != nil {
							opt.OnReminder(remind)
						}
					}
					continue
				}
			}
			return produced, nil
		}
		stepMsgs := []provider.Message{msg}
		results, aborted := runToolCalls(ctx, byName, msg.ToolCalls, opt)
		for _, tm := range results {
			convo = append(convo, tm)
			produced = append(produced, tm)
			stepMsgs = append(stepMsgs, tm)
			if opt.OnMessage != nil {
				opt.OnMessage(tm)
			}
		}
		// A cancelled call is left open. The kernel seals it with one tombstone
		// instead of recording "error: context canceled" as if the tool had run.
		if aborted {
			return produced, ctx.Err()
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
		if opt.Guard != nil {
			remind, stop, reason := opt.Guard(step, stepMsgs)
			if remind != "" {
				convo = AttachReminder(convo, remind)
				if opt.OnReminder != nil {
					opt.OnReminder(remind)
				}
			}
			if stop {
				return stopForReason(ctx, p, convo, produced, opt, reason)
			}
		}
	}
	if opt.OnCap != nil {
		opt.OnCap()
	}
	return stopSummary(ctx, p, convo, produced, opt)
}

func indexTools(list []tools.Tool) (map[string]tools.Tool, []provider.ToolDef, error) {
	byName := make(map[string]tools.Tool, len(list))
	defs := make([]provider.ToolDef, 0, len(list))
	for _, t := range list {
		if _, ok := byName[t.Name()]; ok {
			return nil, nil, fmt.Errorf("agent: duplicate tool %q", t.Name())
		}
		byName[t.Name()] = t
		defs = append(defs, provider.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}
	return byName, defs, nil
}

// AttachReminder appends text to the newest message. A trailing system message
// is left intact and the text becomes its own user message, so the stable
// prefix does not move. Earlier messages are copied unchanged.
func AttachReminder(msgs []provider.Message, text string) []provider.Message {
	text = strings.TrimSpace(text)
	if text == "" {
		return msgs
	}
	out := append([]provider.Message(nil), msgs...)
	if len(out) == 0 || out[len(out)-1].Role == provider.RoleSystem {
		return append(out, provider.Message{Role: provider.RoleUser, Content: text})
	}
	last := out[len(out)-1]
	if last.Content == "" {
		last.Content = text
	} else {
		last.Content += "\n\n" + text
	}
	out[len(out)-1] = last
	return out
}

// stopForReason asks for a summary after the circuit breaker trips.
// The reason note is part of the returned messages so the transcript keeps it.
func stopForReason(ctx context.Context, p provider.Provider, convo, produced []provider.Message, opt Options, reason string) ([]provider.Message, error) {
	if err := ctx.Err(); err != nil {
		return produced, err
	}
	if opt.OnStop != nil && reason != "" {
		opt.OnStop(reason)
	}
	note := provider.Message{Role: provider.RoleUser, Content: StopNote(reason)}
	convo = append(convo, note)
	produced = append(produced, note)
	if opt.OnMessage != nil {
		opt.OnMessage(note)
	}
	msg, err := p.Chat(ctx, provider.Request{Messages: convo}, opt.OnDelta)
	if err != nil {
		return produced, fmt.Errorf("agent: stopped (%s): %w", reason, err)
	}
	if msg.Role == "" {
		msg.Role = provider.RoleAssistant
	}
	msg.ToolCalls = nil
	if strings.TrimSpace(msg.Content) == "" {
		msg.Content = "Stopped (" + reason + ") before the task finished."
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

// StopNote is the transcript line written when the circuit breaker hard-stops.
func StopNote(reason string) string {
	if reason == "" {
		reason = "unspecified"
	}
	return "<stop_reason>" + reason + "</stop_reason>\nStop. Do not call tools. Summarize what was done, what is still pending, and why you stopped."
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
func runToolCalls(ctx context.Context, byName map[string]tools.Tool, calls []provider.ToolCall, opt Options) ([]provider.Message, bool) {
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
	aborted := ctx.Err() != nil
	msgs := make([]provider.Message, 0, len(calls))
	for i, tc := range calls {
		if aborted && strings.TrimSpace(out[i]) == "error: "+ctx.Err().Error() {
			continue
		}
		msgs = append(msgs, provider.Message{
			Role:       provider.RoleTool,
			ToolCallID: tc.ID,
			Name:       tc.Function.Name,
			Content:    out[i],
		})
	}
	return msgs, aborted && len(msgs) < len(calls)
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
		approved, err := opt.Approve(withToolArgs(ctx, raw), t.Name(), summary)
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
	if opt.Redact != nil {
		out = opt.Redact(out)
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
