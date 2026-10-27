// Package agent runs the model/tool loop. It does not know about bots, links, or memory.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"xbot/internal/clip"
	"xbot/internal/notable"
	"xbot/internal/provider"
	"xbot/internal/review"
	"xbot/internal/tools"
)

const defaultMaxSteps = 8
const defaultMaxResult = 64 << 10

const (
	maxTurnImages     = 4
	maxTurnImageBytes = 10 << 20
	imageCapNote      = "[image omitted: turn image cap]"
)

// Approver decides whether a confirmed tool call may run.
type Approver func(ctx context.Context, toolName, summary string) (bool, error)

type toolArgsKey struct{}
type toolCallIDKey struct{}
type toolSummaryKey struct{}

func ToolSummary(ctx context.Context) string { s, _ := ctx.Value(toolSummaryKey{}).(string); return s }

// ToolCallID identifies the call whose approval is being decided.
func ToolCallID(ctx context.Context) string {
	id, _ := ctx.Value(toolCallIDKey{}).(string)
	return id
}

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
	// ToolBatch scopes an interjection signal to exactly this batch, including
	// calls that are still being approved or have not registered their job yet.
	ToolBatch func(context.Context) (context.Context, func())
	RunTool   func(context.Context, tools.Tool, json.RawMessage, func(context.Context) tools.CallResult) tools.CallResult
	MaxSteps  int
	MaxResult int
	Approve   Approver
	OnDelta   func(string)
	// OnTool and OnResult may run at the same time, once per call.
	// id is the provider call ID, so same-name parallel calls can be paired.
	// Callers that stream them (the terminal printer, the web SSE writer) must tolerate that.
	OnTool        func(id, name, summary string)
	OnToolDisplay func(id, name, summary, description string)
	OnResult      func(id, name, output string)
	OnMessage     func(provider.Message)
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
	// Preempt hands the caller a cancel for the model call about to go out,
	// so news that must be read first can stop it; the returned func runs
	// when the call is over. After a call stopped that way, Preempted returns
	// the messages to add, and the same step is sent again with them. Nil
	// leaves every call to run to its end.
	Preempt   func(stop context.CancelFunc) (over func())
	Preempted func() []provider.Message
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
	var images imageBudget
	images.consume(messages)
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
		var msg provider.Message
		for {
			callCtx, stop := context.WithCancel(ctx)
			over := func() {}
			if opt.Preempt != nil {
				over = opt.Preempt(stop)
			}
			msg, err = provider.Complete(callCtx, p, provider.Request{Messages: convo, Tools: defs}, opt.OnDelta)
			over()
			// A call that finished before the stop landed keeps its answer;
			// the news is read at the next boundary instead.
			stopped := err != nil && callCtx.Err() != nil && ctx.Err() == nil
			stop()
			if stopped && opt.Preempted != nil {
				// The call was stopped for something newer, not by the run's
				// own context. Whatever it streamed is dropped with it.
				if extra := opt.Preempted(); len(extra) > 0 {
					convo, produced = addMessages(convo, produced, extra, &images, opt.OnMessage)
					continue
				}
			}
			break
		}
		if err != nil {
			return produced, err
		}
		// The newest image set stays for later steps in this run. Older sets are
		// dropped once a newer view attaches. The transcript does not store
		// the bytes, and the kernel drops whatever remains when the turn ends.
		if msg.Role == "" {
			msg.Role = provider.RoleAssistant
		}
		convo = append(convo, msg)
		produced = append(produced, msg)
		if opt.OnMessage != nil {
			opt.OnMessage(msg)
		}
		if len(msg.Calls()) == 0 {
			if opt.AfterStep != nil && opt.Preempt != nil && step+1 < opt.MaxSteps {
				extra, err := opt.AfterStep()
				if err != nil {
					return produced, err
				}
				if len(extra) > 0 {
					convo, produced = addMessages(convo, produced, extra, &images, opt.OnMessage)
					continue
				}
			}
			if opt.OnIdle != nil && step+1 < opt.MaxSteps {
				remind, cont := opt.OnIdle(msg)
				if cont {
					if remind != "" {
						note := provider.Message{Role: provider.RoleUser, Internal: true, Content: provider.TextContent(strings.TrimSpace(remind))}
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
		results, aborted := runToolCalls(ctx, byName, msg.Calls(), opt, &images)
		convoBase := len(convo)
		producedBase := len(produced)
		for _, tm := range results {
			convo = append(convo, tm)
			produced = append(produced, tm)
			stepMsgs = append(stepMsgs, tm)
			if opt.OnMessage != nil {
				opt.OnMessage(tm)
			}
		}
		if hasImages(results) {
			images.release(dropImages(convo[:convoBase]))
			dropImages(produced[:producedBase])
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
			convo, produced = addMessages(convo, produced, extra, &images, opt.OnMessage)
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
		return append(out, provider.Message{Role: provider.RoleUser, Content: provider.TextContent(text)})
	}
	last := out[len(out)-1]
	if last.Text() == "" {
		last.SetText(text)
	} else {
		last.SetText(last.Text() + "\n\n" + text)
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
	note := provider.Message{Role: provider.RoleUser, Internal: true, Content: provider.TextContent(StopNote(reason))}
	convo = append(convo, note)
	produced = append(produced, note)
	if opt.OnMessage != nil {
		opt.OnMessage(note)
	}
	msg, err := provider.Complete(ctx, p, provider.Request{Messages: convo}, opt.OnDelta)
	if err != nil {
		return produced, fmt.Errorf("agent: stopped (%s): %w", reason, err)
	}
	if msg.Role == "" {
		msg.Role = provider.RoleAssistant
	}
	msg.SetCalls(nil)
	if strings.TrimSpace(msg.Text()) == "" {
		msg.SetText("Stopped (" + reason + ") before the task finished.")
		if opt.OnDelta != nil {
			opt.OnDelta(msg.Text())
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
	convo = append(convo, provider.Message{Role: provider.RoleUser, Internal: true, Content: provider.TextContent(note)})
	msg, err := provider.Complete(ctx, p, provider.Request{Messages: convo}, opt.OnDelta)
	if err != nil {
		return produced, fmt.Errorf("agent: max steps (%d) reached: %w", opt.MaxSteps, err)
	}
	if msg.Role == "" {
		msg.Role = provider.RoleAssistant
	}
	msg.SetCalls(nil)
	if strings.TrimSpace(msg.Text()) == "" {
		msg.SetText(fmt.Sprintf("Stopped after %d steps, before the task finished. The last response still requested tools, so they were not run.", opt.MaxSteps))
		if opt.OnDelta != nil {
			opt.OnDelta(msg.Text())
		}
	}
	produced = append(produced, msg)
	if opt.OnMessage != nil {
		opt.OnMessage(msg)
	}
	return produced, nil
}

type toolExec struct {
	text   string
	images []provider.Image
}

// runToolCalls starts every call, then returns tool messages in call order.
// The approval lock is held only around Approve, so a prompt does not stall the others.

func runToolCalls(ctx context.Context, byName map[string]tools.Tool, calls []provider.ToolCall, opt Options, budget *imageBudget) ([]provider.Message, bool) {
	if opt.ToolBatch != nil {
		var done func()
		ctx, done = opt.ToolBatch(ctx)
		defer done()
	}
	out := make([]toolExec, len(calls))
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
		if aborted && strings.TrimSpace(out[i].text) == "error: "+ctx.Err().Error() {
			continue
		}
		imgs, note := budget.take(out[i].images)
		text := out[i].text
		if note != "" {
			if text != "" && !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
			text += note
		}
		msgs = append(msgs, provider.Message{Role: provider.RoleTool, Name: tc.Function.Name, Images: imgs, Content: provider.ToolResult(tc.ID, text)})
	}
	return msgs, aborted && len(msgs) < len(calls)
}

func execTool(ctx context.Context, byName map[string]tools.Tool, tc provider.ToolCall, opt Options, approve *sync.Mutex) (res toolExec) {
	defer func() {
		if r := recover(); r != nil {
			text := fmt.Sprintf("error: tool panic: %v", r)
			if opt.Redact != nil {
				text = opt.Redact(text)
			}
			text, _ = clip.Bytes(text, opt.MaxResult)
			res = toolExec{text: finish(opt, tc.ID, tc.Function.Name, text)}
		}
	}()
	t, ok := byName[tc.Function.Name]
	if !ok {
		return toolExec{text: finish(opt, tc.ID, tc.Function.Name, fmt.Sprintf("error: unknown tool %q", tc.Function.Name))}
	}
	raw := json.RawMessage(tc.Function.Arguments)
	if !json.Valid(raw) {
		return toolExec{text: finish(opt, tc.ID, t.Name(), "error: tool arguments are not valid JSON")}
	}
	need, summary, err := tools.ConfirmCall(ctx, t, raw)
	if err != nil {
		return toolExec{text: finish(opt, tc.ID, t.Name(), "error: "+err.Error())}
	}
	if opt.OnTool != nil {
		opt.OnTool(tc.ID, t.Name(), summary)
	}
	if opt.OnToolDisplay != nil {
		var args struct {
			Description string `json:"description"`
		}
		_ = json.Unmarshal(raw, &args)
		opt.OnToolDisplay(tc.ID, t.Name(), summary, args.Description)
	}
	if need {
		if opt.Approve == nil {
			return toolExec{text: finish(opt, tc.ID, t.Name(), "not run: no approver available on this link")}
		}
		approved, err := func() (bool, error) {
			approve.Lock()
			defer approve.Unlock()
			return opt.Approve(withToolArgs(context.WithValue(ctx, toolCallIDKey{}, tc.ID), raw), t.Name(), summary)
		}()
		if err != nil {
			// An error that names its own tool result (a parked card) is
			// shown as that, not as a failure.
			var res interface{ ToolResult() string }
			if errors.As(err, &res) {
				return toolExec{text: finish(opt, tc.ID, t.Name(), res.ToolResult())}
			}
			return toolExec{text: finish(opt, tc.ID, t.Name(), "error: "+err.Error())}
		}
		if !approved {
			return toolExec{text: finish(opt, tc.ID, t.Name(), "denied by user")}
		}
	}
	if err := ctx.Err(); err != nil {
		return toolExec{text: finish(opt, tc.ID, t.Name(), "error: "+err.Error())}
	}
	ctx = withToolArgs(context.WithValue(ctx, toolCallIDKey{}, tc.ID), raw)
	ctx = context.WithValue(ctx, toolSummaryKey{}, summary)
	run := func(ctx context.Context) (string, error) { return t.Run(ctx, raw) }
	if prep, ok := t.(interface {
		PrepareRun(context.Context, json.RawMessage) (tools.PreparedRun, error)
	}); ok {
		p, err := prep.PrepareRun(ctx, raw)
		if err != nil {
			return toolExec{text: finish(opt, tc.ID, t.Name(), "error: "+err.Error())}
		}
		if p.Run == nil {
			return toolExec{text: finish(opt, tc.ID, t.Name(), p.Text)}
		}
		run = p.Run
	}
	invoke := func(ctx context.Context) (res tools.CallResult) {
		defer func() {
			if p := recover(); p != nil {
				res = tools.CallResult{Text: fmt.Sprintf("error: tool panic: %v", p)}
				if opt.Redact != nil {
					res.Text = opt.Redact(res.Text)
				}
				res.Text, _ = clip.Bytes(res.Text, opt.MaxResult)
			}
		}()
		ctx, commit := notable.Capture(ctx)
		ctx, sink := tools.WithImageSink(ctx)
		out, err := run(ctx)
		if err != nil {
			out = "error: " + err.Error()
		} else {
			hint, ok := t.(review.ReadOnlyHinter)
			notable.Tool(ctx, t.Name(), raw, out, ok && hint.ReadOnlyHint())
			if commit != nil && !strings.HasPrefix(strings.TrimSpace(out), "error:") {
				commit()
				res.Notable = notable.Items(ctx)
			}
			res.Images = sink.Take()
		}
		if strings.TrimSpace(out) == "" {
			out = "(empty)"
		}
		if opt.Redact != nil {
			out = opt.Redact(out)
		}
		res.Text, _ = clip.Bytes(out, opt.MaxResult)
		return res
	}
	var result tools.CallResult
	if opt.RunTool != nil {
		result = opt.RunTool(ctx, t, raw, invoke)
	} else {
		result = invoke(ctx)
	}
	if result.Err != nil {
		result.Text = "error: " + result.Err.Error()
		if opt.Redact != nil {
			result.Text = opt.Redact(result.Text)
		}
	}
	var images []provider.Image
	for _, img := range result.Images {
		images = append(images, provider.Image{MIME: img.MIME, Data: img.Data})
	}
	return toolExec{text: finish(opt, tc.ID, t.Name(), result.Text), images: images}
}

type imageBudget struct {
	n     int
	bytes int
}

// consume counts images already on the conversation. A later run in the same
// turn (a step-cap continuation, a subagent's next round) must not attach
// another full budget on top of pictures that are still being sent.
func (b *imageBudget) consume(msgs []provider.Message) {
	if b == nil {
		return
	}
	for _, m := range msgs {
		for _, img := range m.Images {
			b.n++
			b.bytes += len(img.Data)
		}
	}
}

func (b *imageBudget) release(n, bytes int) {
	if b == nil {
		return
	}
	b.n -= n
	b.bytes -= bytes
	if b.n < 0 {
		b.n = 0
	}
	if b.bytes < 0 {
		b.bytes = 0
	}
}

func (b *imageBudget) take(imgs []provider.Image) ([]provider.Image, string) {
	if b == nil || len(imgs) == 0 {
		return imgs, ""
	}
	keep := make([]provider.Image, 0, len(imgs))
	dropped := false
	for _, img := range imgs {
		if b.n >= maxTurnImages || b.bytes+len(img.Data) > maxTurnImageBytes {
			dropped = true
			continue
		}
		keep = append(keep, img)
		b.n++
		b.bytes += len(img.Data)
	}
	if !dropped {
		return keep, ""
	}
	return keep, imageCapNote
}

func hasImages(msgs []provider.Message) bool {
	for _, m := range msgs {
		if len(m.Images) > 0 {
			return true
		}
	}
	return false
}

// dropImages clears pictures on msgs and reports how many bytes the budget had counted.
func dropImages(msgs []provider.Message) (n, bytes int) {
	for i := range msgs {
		for _, img := range msgs[i].Images {
			n++
			bytes += len(img.Data)
		}
		msgs[i].Images = nil
	}
	return n, bytes
}

func finish(opt Options, id, name, out string) string {
	if opt.OnResult != nil {
		opt.OnResult(id, name, out)
	}
	return out
}

// addMessages appends messages that arrived during the run (absorbed or
// interjected) to the conversation and to what the run produced.
func addMessages(convo, produced, extra []provider.Message, images *imageBudget, onMessage func(provider.Message)) ([]provider.Message, []provider.Message) {
	for _, m := range extra {
		// A message absorbed mid-turn can carry pictures of its own. They
		// go through the same per-turn allowance a tool's pictures do, so
		// the request never grows past it.
		//
		// The text is not touched here. The kernel put the note naming
		// these pictures into the message before handing it over, and the
		// note already says the picture may need opening by path; adding a
		// second one would edit a line the transcript is about to store,
		// and the stored text could no longer be cleaned back to the
		// owner's own words.
		if len(m.Images) > 0 {
			m.Images, _ = images.take(m.Images)
		}
		convo = append(convo, m)
		produced = append(produced, m)
		if onMessage != nil {
			onMessage(m)
		}
	}
	return convo, produced
}
