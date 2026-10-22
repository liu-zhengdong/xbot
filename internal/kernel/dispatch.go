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
	"xbot/internal/exec"
	"xbot/internal/provider"
	"xbot/internal/review"
	"xbot/internal/tools"
)

// The foreground understands, plans, and dispatches; typed subagents do the
// work (ADR 0016, docs/atlas/subagents.md). This file is the dispatch tool, the
// tool set each type gets, and the context guard on a subagent's requests.

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
	b.WriteString("Hand a task to a subagent of the chosen type and return at once; its result arrives later as an event. The subagent starts with an empty context: the task must say everything it needs (goal, files, constraints, what done looks like). Types:")
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
		"type": map[string]any{"type": "string", "enum": names, "description": "subagent type"},
		"task": stringProp("the full task description"),
	}, []string{"type", "task"})
}
func (dispatchTool) Confirm(json.RawMessage) (bool, string, error) { return false, "dispatch", nil }
func (t dispatchTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Type string `json:"type"`
		Task string `json:"task"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	return t.k.dispatch(ctx, in.Type, in.Task)
}

// dispatch starts a subagent of the named type. Its system prompt is the base
// subagent rules, the type's prompt and report sections, and the memory
// snapshot of the person this turn serves; the task is the first message.
func (k *Kernel) dispatch(ctx context.Context, typeName, task string) (string, error) {
	ty, ok := agenttype.Find(k.agentTypes(), strings.TrimSpace(typeName))
	if !ok {
		return "", fmt.Errorf("dispatch: unknown type %q", typeName)
	}
	task = strings.TrimSpace(task)
	if task == "" {
		return "", errors.New("dispatch: task is empty")
	}
	if len(task) > 16<<10 {
		return "", errors.New("dispatch: task is too long")
	}
	id, err := newAgentID()
	if err != nil {
		return "", err
	}
	persona := ty.Instructions(exec.SubPersona)
	if mem := k.dispatchMemory(ctx); mem != "" {
		persona += "\n\nWhat is known about the people you work for and about this bot (a read-only snapshot of memory):\n" + mem
	}
	cwd := k.cwdFor(ctx)
	k.adoptScope(ctx, id)
	req := exec.StartRequest{
		ID:         id,
		Bot:        k.Bot.Name(),
		Task:       task,
		Mode:       exec.ModeBrief,
		Dir:        k.Bot.AgentDir(id),
		Type:       ty.Name,
		Persona:    persona,
		TimeoutSec: int(ty.Budget / time.Second),
		Resource:   writeResource(ty, cwd),
	}
	live, err := k.ensureExec().Start(ctx, req)
	if err != nil {
		k.dropScope(id)
		return "", err
	}
	if live.Status == "queued" {
		return fmt.Sprintf("%s as %s (budget %s). Tell the owner it is queued, not started. It starts automatically when a slot and its resource are free; do not do the task yourself.", startReceipt(live), ty.Name, ty.Budget), nil
	}

	return fmt.Sprintf("%s as %s (budget %s). Its result arrives as an event; tell the owner it has started, and do not do the task yourself.", startReceipt(live), ty.Name, ty.Budget), nil
}

// writeResource is what a subagent of type ty may write when it starts in
// cwd: the git checkout around cwd, or cwd itself. Two subagents with the
// same resource queue (exec.Local); one that cannot write has none.
func writeResource(ty agenttype.Type, cwd string) string {
	if !ty.Allows("write_file") && !ty.Allows("edit_file") && !ty.Allows("shell") {
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
		case t.Name() == "shell" && ty.Allows(agenttype.ShellReadOnly):
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
	if !review.ShellIsReadOnly(args) {
		return "", errors.New("shell: this subagent may only run plain read commands; no redirection, chaining, or writes")
	}
	return s.Tool.Run(ctx, args)
}

// subApprover routes a subagent's confirmation to the owner when the host can
// ask without a live request (the web card), and otherwise falls back to the
// background switch.
func (k *Kernel) subApprover() agent.Approver {
	if k.Permissive {
		// No cards (ADR 0019): a subagent's calls run like the owner's own.
		return func(context.Context, string, string) (bool, error) { return true, nil }
	}
	if k.BackgroundApprove != nil {
		return func(ctx context.Context, tool, summary string) (bool, error) {
			return k.trackApprove(k.BackgroundApprove)(ctx, tool, summary)
		}
	}
	return k.subApprove()
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
	msg, err := p.inner.Chat(ctx, p.k.fit(ctx, nil, p.fit, raw, false), onDelta)
	if err != nil && provider.IsContextOverflow(err) {
		msg, err = p.inner.Chat(ctx, p.k.fit(ctx, nil, p.fit, raw, true), onDelta)
	}
	return msg, err
}

func (k *Kernel) wrapSub(_ exec.StartRequest, p provider.Provider, pre int) provider.Provider {
	f := newTurnFit(pre, nil)
	// A subagent's transcript is its own file, which expand does not read.
	f.noSeq = "Read the file or run the call again if you still need it."
	return &fitProvider{inner: p, k: k, fit: f}
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
