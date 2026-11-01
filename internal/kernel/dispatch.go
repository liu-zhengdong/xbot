package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"xbot/internal/agent"
	"xbot/internal/agenttype"
	"xbot/internal/compact"
	"xbot/internal/config"
	"xbot/internal/exec"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/review"
	"xbot/internal/tools"
)

// The main line has every tool and does short work itself; long work goes to
// typed subagents through dispatch (ADR 0028, docs/atlas/subagents.md). This
// file is the dispatch rule, the dispatch tool, the tool set each type gets,
// and the context guard on a subagent's requests.

// DispatchRule tells the model to answer the owner before working and to hand
// long work out. It is added to the rules only while dispatch is on.
const DispatchRule = "先回应用户，再干活：调用要花点时间的工具前，用一两句话说你理解了什么、接下来要做什么。你拥有所有工具，短活可以自己做。长活（步骤多、构建或调研耗时长、任何不该让用户等的事）交给子代理：用 dispatch 选对类型，任务里写清子代理需要的一切，因为它从空上下文开始（目标、路径、约束、怎样算做完）。把子代理当成能自己判断的同事：交代目标、约束、验收标准和必要背景，步骤由它自己定，它可以反问；不要写逐步剧本，也不要要求它「只输出…」。然后用一两句话告诉用户你交出去了什么。子代理的结果回来后，告诉用户结果如何，值得留的记进记忆。结果分批回来时，只报告新到的那个；已经告诉过用户的细节不要再说。需要总结时，用一句话带过之前的结果，不再列它们的细节。不要派发没人要求的验证或重复调研；已商定的验收检查由你自己看证据；拿不到证据就报告为未验证，只有要扩大范围时才先问。对子代理的补充与后续反馈用 agent_message（steer 或 queue），完成后同 id 仍能继续；停止用 agent_stop。同一件事不要再开第二个。创建默认 fresh；fork 只继承已完成轮，当前轮的新信息仍须写进任务。"

// legacyDispatchRule is the ADR 0016 wording. It is only recognized, so a
// bot rules file that still carries it drops it on migration.
const legacyDispatchRule = "You are the foreground: you talk with the owner, work out what they want, plan, hand work out, report results, and keep memory. You do not do the work yourself."

// dispatching reports whether dispatch is offered. It needs the agents link:
// without it the work would have nowhere to go.
func (k *Kernel) dispatching() bool {
	return k != nil && k.Dispatch && k.hasLink(link.NameAgents)
}

// agentTypes is the built-in types overlaid by the bot's own type files.
// A file that does not parse is skipped; the others still load.
func (k *Kernel) agentTypes() []agenttype.Type {
	types, _ := agenttype.Load(k.Bot.AgentTypesDir())
	return types
}

type dispatchTool struct{ k *Kernel }

func (dispatchTool) Name() string { return "dispatch" }
func (t dispatchTool) Description() string {
	var b strings.Builder
	b.WriteString("Hand a task to a subagent of the chosen type and return at once; its result arrives later as an event. Default fresh starts with an empty context; fork inherits completed parent turns only. Include goal, files, constraints, and what done looks like in the task. Completed ids can continue via agent_message. Types:")
	for _, ty := range t.k.agentTypes() {
		fmt.Fprintf(&b, "\n- %s: %s", ty.Name, ty.When)
	}
	return b.String()
}
func (t dispatchTool) Parameters() any {
	names := []string{}
	for _, ty := range t.k.agentTypes() {
		names = append(names, ty.Name)
	}
	return schema(map[string]any{
		"type":             map[string]any{"type": "string", "enum": names, "description": "subagent type"},
		"task":             stringProp("交给子代理的事：写清意图、约束、验收标准和必要背景；不要写逐步剧本，也不要「只输出…」之类的格式指令"),
		"context":          map[string]any{"type": "string", "enum": []string{"fresh", "fork"}, "description": "Default fresh; fork inherits completed parent turns, excluding the current turn"},
		"provider":         stringProp("Configured provider; omitted inherits the parent"),
		"model":            stringProp("Configured model id"),
		"thinking":         map[string]any{"type": "string", "enum": []string{"on", "off", "omit"}},
		"reasoning_effort": map[string]any{"type": "string", "enum": []string{"low", "high", "max"}},
	}, []string{"type", "task"})
}
func (dispatchTool) Confirm(json.RawMessage) (bool, string, error) { return false, "dispatch", nil }
func (t dispatchTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in SubagentRequest
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	live, err := t.k.StartSubagent(ctx, in)
	if err != nil {
		return "", err
	}
	return dispatchReceipt(live, in.Type), nil
}

// dispatch remains the existing internal caller's brief shorthand.
func (k *Kernel) dispatch(ctx context.Context, typeName, task string) (string, error) {
	live, err := k.StartSubagent(ctx, SubagentRequest{Type: typeName, Task: task})
	if err != nil {
		return "", err
	}
	return dispatchReceipt(live, typeName), nil
}
func dispatchReceipt(live exec.Task, typeName string) string {
	receipt := startReceipt(live) + "，类型 " + typeName
	if live.Status == "queued" {
		return receipt + "。告诉用户它在排队、还没开始；有空位和资源时会自动开始，不要自己重复做这件事。"
	}
	return receipt + "。结果会作为事件送回；告诉用户它已经开始。后续反馈用 agent_message 联系同一个 id。"
}

// writeResource is what a subagent of type ty may write when it starts in
// cwd: the git checkout around cwd, or cwd itself. Two subagents with the
// same resource queue (exec.Local); one that cannot write has none.
func writeResource(ty agenttype.Type, cwd string) string {
	if !ty.Allows("write_file") && !ty.Allows("edit_file") && !ty.AllowsShell() {
		return ""
	}
	if cwd == "" {
		return ""
	}
	for dir := filepath.Clean(cwd); ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return "repo:" + dir
		}
		if parent := filepath.Dir(dir); parent == dir {
			break
		}
	}
	return "dir:" + filepath.Clean(cwd)
}

// dispatchMemory is the memory snapshot of this turn's view, or empty.
func (k *Kernel) dispatchMemory(ctx context.Context) string {
	st, err := k.Bot.LoadState()
	if err != nil {
		return ""
	}
	st = migrate(st)
	mem, err := k.memSnapFor(ctx, st, "")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(mem)
}

// toolsFor is the tool set of one subagent: the whole mounted directory,
// filtered by its type. A request without a type (a plain spawn) gets the
// whole set.
func (k *Kernel) toolsFor(req exec.StartRequest) []tools.Tool {
	if req.Descriptor != nil {
		return k.continuableTools(req)
	}
	base := k.subagentBase(req.ID)
	if req.Type == "" {
		return base
	}
	ty, ok := agenttype.Find(k.agentTypes(), req.Type)
	if !ok {
		return nil
	}
	var out []tools.Tool
	for _, t := range base {
		switch {
		case ty.Allows(t.Name()):
			out = append(out, t)
		case review.IsShellTool(t.Name()) && ty.Allows(agenttype.ShellReadOnly):
			out = append(out, readOnlyShell{t})
		}
	}
	return out
}

// readOnlyShell is the shell for a type that may only look: a command that is
// not a plain read is refused before it runs.
type readOnlyShell struct{ tools.Tool }

func (s readOnlyShell) Description() string {
	return s.Tool.Description() + " Only plain read commands run here (ls, cat, grep, git status/log/diff/show and the like, no redirection or chaining)."
}

func (s readOnlyShell) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if !review.ShellToolIsReadOnly(s.Name(), args) {
		return "", errors.New("shell: 这个子代理只能运行单纯的读取命令，不能重定向、串联命令或写入")
	}
	return s.Tool.Run(ctx, args)
}

// subApprover routes a subagent's confirmation to the owner when the host can
// ask without a live request (the web card), and otherwise falls back to the
// background switch.
func (k *Kernel) subApprover() agent.Approver {
	return func(ctx context.Context, tool, summary string) (bool, error) {
		if k.permissive() {
			return true, nil
		}
		if k.BackgroundApprove != nil {
			return k.trackApprove(k.BackgroundApprove)(ctx, tool, summary)
		}
		return k.subApprove()(ctx, tool, summary)
	}
}

// fitProvider keeps one subagent's requests under the context lines, the way
// a turn's own requests are kept (fit.go): a subagent can run for hours.
type fitProvider struct {
	inner provider.Provider
	k     *Kernel
	fit   *turnFit
}

func (p *fitProvider) Chat(ctx context.Context, req provider.Request, onDelta func(string)) (provider.Message, error) {
	raw := req
	schemaAttempt := &mcpSchemaAttempt{k: p.k}
	request, err := p.fitted(ctx, raw, false)
	if err != nil {
		return provider.Message{}, err
	}
	_, msg, err := schemaAttempt.chat(ctx, request, onDelta, p.inner.Chat)
	if err != nil && provider.IsContextOverflow(err) {
		request, err = p.fitted(ctx, raw, true)
		if err == nil {
			_, msg, err = schemaAttempt.chat(ctx, request, onDelta, p.inner.Chat)
		}
	}
	if err == nil && p.fit.budget != nil && msg.Usage.Reported {
		p.fit.mu.Lock()
		p.fit.budget.ratio = calibrate(msg.Usage.PromptTokens, compact.EstimateMessages(request.Messages)+estimateTools(request.Tools))
		p.fit.mu.Unlock()
	}
	return msg, err
}

func (k *Kernel) wrapSub(req exec.StartRequest, p provider.Provider, pre int) provider.Provider {
	f := newTurnFit(pre, nil)
	f.noSeq = "Read the file or run the call again if you still need it."
	if req.Descriptor != nil {
		f.noSeq = "Read this child's transcript with expand_subagent or read_file for the original result; do not rerun completed side effects."
		window := compact.DefaultWindow
		d := *req.Descriptor
		if d.Provider != "" {
			if _, spec, err := config.Lookup(k.Bot.Root, d.Provider, d.Model); err == nil {
				window = spec.ContextWindow()
			}
		}
		if limit := k.Bot.Config().ContextLimit; limit > 0 && limit < window {
			window = limit
		}
		target, ceiling := compact.Lines(window)
		if d.Provider == "" {
			target, ceiling = k.lines()
		} // injected fake budgets
		f.budget = &subagentBudget{target: target, ceiling: ceiling, ratio: 1, compactor: compact.LLM{P: p}}
		// Recovered history must fit too; preserve the child's system prefix,
		// then summarize only a safe prefix before its latest step.
		f.pre = 1
	}
	return &fitProvider{inner: p, k: k, fit: f}
}

func (p *fitProvider) fitted(ctx context.Context, req provider.Request, overflow bool) (provider.Request, error) {
	out := p.k.fit(ctx, nil, p.fit, req, overflow)
	if p.fit.budget != nil {
		p.fit.mu.Lock()
		size := p.fit.scaled(p.k, compact.EstimateMessages(out.Messages)+estimateTools(out.Tools))
		ceiling := p.fit.budget.ceiling
		p.fit.mu.Unlock()
		if size > ceiling {
			return out, fmt.Errorf("subagent context exceeds its model window after fitting (%d > %d)", size, ceiling)
		}
	}
	return out, nil
}

// maxTypeBudget is the longest budget among the types: the executor's cap.
func (k *Kernel) maxTypeBudget() time.Duration {
	max := time.Duration(0)
	for _, t := range k.agentTypes() {
		if t.Budget > max {
			max = t.Budget
		}
	}
	if max <= 0 {
		max = defaultBackgroundMaxTime
	}
	return max
}
