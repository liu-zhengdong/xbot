package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"xbot/internal/agenttype"
	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/config"
	"xbot/internal/exec"
	"xbot/internal/inbox"
	"xbot/internal/provider"
	"xbot/internal/review"
	"xbot/internal/tools"
	"xbot/internal/work"
	"xbot/prompts"
)

// SubagentRequest selects one local collaborator. Its route is resolved once at
// creation; subsequent turns re-resolve credentials, never the parent's binding.
type SubagentRequest struct {
	Type            string `json:"type"`
	Task            string `json:"task"`
	Context         string `json:"context,omitempty"`
	Provider        string `json:"provider,omitempty"`
	Model           string `json:"model,omitempty"`
	Thinking        string `json:"thinking,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

func (k *Kernel) StartSubagent(ctx context.Context, in SubagentRequest) (exec.Task, error) {
	if k.needsEndpoint() {
		return exec.Task{}, errors.New("可继续子代理目前仅支持本机执行")
	}
	ty, ok := agenttype.Find(k.agentTypes(), strings.TrimSpace(in.Type))
	if !ok {
		return exec.Task{}, fmt.Errorf("dispatch: unknown type %q", in.Type)
	}
	in.Task = strings.TrimSpace(in.Task)
	if in.Task == "" || len(in.Task) > 16<<10 {
		return exec.Task{}, errors.New("dispatch: task must contain 1–16384 bytes")
	}
	mode := exec.ModeBrief
	switch in.Context {
	case "", "fresh":
	case "fork":
		mode = exec.ModeFork
	default:
		return exec.Task{}, errors.New("dispatch: context must be fresh or fork")
	}
	id, err := newAgentID()
	if err != nil {
		return exec.Task{}, err
	}
	d := exec.Descriptor{Version: 1, ID: id, Bot: k.currentName(), Type: ty.Name, Mode: mode,
		DeviceID: k.selectedDevice(ctx),
		CWD:      k.cwdFor(ctx), Tools: append([]string(nil), ty.Tools...), MaxSteps: maxStepsGuard,
		TimeoutSec: int(ty.Budget / time.Second)}
	if err := k.selectSubagentRoute(&d, in); err != nil {
		return exec.Task{}, err
	}
	var ownDir string
	if err := k.withCurrentIO(func(current *bot.Bot, _ *inbox.Inbox) error { ownDir = current.AgentDir(id); return nil }); err != nil {
		return exec.Task{}, err
	}
	persona := ty.Instructions(exec.SubPersona)
	persona += "\n\n需要提前沟通时调用 send_message_to_parent；结束时运行时会自动交回结果。不要直接向用户发消息。"
	persona += fmt.Sprintf("\n\n你的 id 是 %s。自己的原文记录在 %s；在 Shell 用 \"$XBOT_BIN\" subagent read --self <id>、expand --self <id> 查看原文，list --self 列出全部孩子。", id, ownDir)
	if ty.Name != "research" {
		if guide := k.cliProfileGuide(); guide != "" {
			persona += "\n\n" + guide
		}
	} else if k.cliProfileGuide() != "" {
		persona += `
设备只读入口："$XBOT_BIN" device --help；device list --self --json 查看真实设备，read --self --args '{"path":"file"}' 读取文件，tasks/task REF --self 查看任务，artifact REF --self --artifact-id ID 取有界产物。默认继承派发消息的设备选择，可用 --device ID 选择账号设备。exec、write、desktop 和 stop 均不可用。`
	}
	if d.DeviceID != "" {
		persona += fmt.Sprintf("\n本任务继承的执行设备是 %q；使用 device --self CLI，普通Shell/文件工具仍作用对话主机。", d.DeviceID)
	}
	if mem := k.dispatchMemory(ctx); mem != "" {
		persona += "\n\n你服务的人和这个 Bot 的已知情况（记忆的只读快照）：\n" + mem
	}
	if d.Provider != "" {
		overlay, err := prompts.Load(k.Bot.Root, d.Provider, d.Model)
		if err != nil {
			return exec.Task{}, err
		}
		if overlay != "" {
			persona += "\n\n## Model\n" + overlay
		}
	}
	req, err := k.restoreSubagent(ctx, d)
	if err != nil {
		return exec.Task{}, err
	}
	req.Task, req.Persona = in.Task, persona
	if mode == exec.ModeFork {
		req.Fork, err = k.completedSubagentHistory(ctx)
		if err != nil {
			return exec.Task{}, err
		}
	}
	task, err := k.ensureExec().Start(ctx, req)
	if err != nil {
		k.dropScope(id)
	}
	return task, err
}

func (k *Kernel) selectSubagentRoute(d *exec.Descriptor, in SubagentRequest) error {
	cfg := k.Bot.Config()
	d.Provider, d.Model = strings.TrimSpace(in.Provider), strings.TrimSpace(in.Model)
	// A fake may sit on a Bot whose creation filled a default registry binding.
	// Only explicit non-endpoint provider injection, with no route override,
	// bypasses registry resolution. Production clients expose their endpoint.
	if d.Provider == "" && d.Model == "" && k.Provider != nil {
		if _, endpoint := provider.EndpointOf(k.Provider); !endpoint {
			if in.Thinking != "" || in.ReasoningEffort != "" {
				return errors.New("dispatch: reasoning overrides require a configured model")
			}
			return nil
		}
	}
	if d.Provider == "" {
		d.Provider = cfg.Provider
	}
	if d.Model == "" && in.Provider == "" {
		d.Model = cfg.Model
	}
	_, model, err := config.Lookup(k.Bot.Root, d.Provider, d.Model)
	if err != nil {
		return err
	}
	d.Model, d.Thinking, d.ReasoningEffort = model.Name, model.Thinking, model.ReasoningEffort
	if in.Thinking != "" {
		d.Thinking = in.Thinking
	}
	if in.ReasoningEffort != "" {
		d.ReasoningEffort = in.ReasoningEffort
	}
	switch d.Thinking {
	case "", "on", "off", "omit":
	default:
		return errors.New("dispatch: thinking must be on, off or omit")
	}
	switch d.ReasoningEffort {
	case "", "low", "high", "max":
	default:
		return errors.New("dispatch: reasoning_effort must be low, high or max")
	}
	_, err = k.subagentEndpoint(*d)
	return err
}

func (k *Kernel) subagentEndpoint(d exec.Descriptor) (provider.Endpoint, error) {
	ep, _, err := config.Resolve(k.Bot.Root, d.Provider, d.Model, config.Getenv)
	if err != nil {
		return ep, err
	}
	ep.Thinking, ep.ReasoningEffort = d.Thinking, d.ReasoningEffort
	return ep, nil
}

func (k *Kernel) subagentProvider(req exec.StartRequest) (provider.Provider, error) {
	if req.Descriptor == nil {
		return k.boundProvider(), nil
	}
	d := *req.Descriptor
	if d.Provider == "" && d.Model == "" && k.Provider != nil {
		if _, endpoint := provider.EndpointOf(k.Provider); !endpoint {
			return k.searchProvider(k.Provider), nil
		}
	}
	ep, err := k.subagentEndpoint(d)
	if err != nil {
		return nil, err
	}
	p, err := provider.New(ep)
	if err != nil {
		return nil, err
	}
	selector, err := config.ModelSelector(k.Bot.Root, d.Provider, p, config.Getenv)
	if err != nil {
		return nil, err
	}
	p, err = selector.Select(context.Background(), provider.Hint{Bot: k.currentName()})
	if err != nil {
		return nil, err
	}
	return k.searchProvider(p), nil
}

func (k *Kernel) restoreSubagent(_ context.Context, d exec.Descriptor) (exec.StartRequest, error) {
	req := exec.StartRequest{ID: d.ID, Mode: d.Mode, Type: d.Type, MaxSteps: d.MaxSteps, TimeoutSec: d.TimeoutSec}
	// The descriptor may have been read before a rename, even when this Kernel
	// already loaded the new name. Rebase its own origin before creating scope;
	// a later request rebase cannot repair a scope that fell back to workspace.
	k.cwd.mu.Lock()
	k.ensureLoadedLocked()
	err := k.withCurrentIO(func(current *bot.Bot, _ *inbox.Inbox) error {
		previous := d.Bot
		d.CWD = k.rebaseOriginalPath(d.CWD, current)
		if previous != "" {
			d.CWD = rebasePath(d.CWD, bot.Dir(current.Root, previous), bot.Dir(current.Root, current.Name()))
			d.CWD = rebasePath(d.CWD, work.BotDir(current.Root, previous), work.BotDir(current.Root, current.Name()))
		}
		k.rebaseCwdLocked(current)
		if k.cwd.extra == nil {
			k.cwd.extra = map[string]string{}
		}
		if _, exists := k.cwd.extra[d.ID]; !exists && d.CWD != "" {
			k.cwd.extra[d.ID] = d.CWD
		}
		d.Bot, req.Bot = current.Name(), current.Name()
		req.Dir, req.Workspace = current.AgentDir(d.ID), k.currentWorkspaceFor(current)
		return nil
	})
	k.cwd.mu.Unlock()
	if err != nil {
		return exec.StartRequest{}, err
	}
	cwd, _ := k.shellStartScope(d.ID)
	d.CWD = cwd
	req.Descriptor = &d
	req.Resource = writeResource(agenttype.Type{Tools: d.Tools}, cwd)
	if jobs := k.shellJobs(); jobs != nil {
		req.JobDir = jobs.Directory()
	}
	return req, nil
}

// MessageSubagent is the authenticated human entry. Model tools use the same
// operation with a runtime-owned Bot sender; neither accepts a claimed sender.
func (k *Kernel) MessageSubagent(ctx context.Context, id, text, delivery string) (exec.Task, error) {
	return k.messageSubagent(ctx, id, text, delivery, "owner")
}
func (k *Kernel) messageSubagent(ctx context.Context, id, text, delivery, sender string) (exec.Task, error) {
	if !execID(id) {
		return exec.Task{}, exec.ErrNotFound
	}
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 16<<10 {
		return exec.Task{}, errors.New("消息须包含 1–16384 字节")
	}
	if delivery == "" {
		delivery = exec.KindSteer
	}
	if delivery != exec.KindSteer && delivery != exec.KindQueue {
		return exec.Task{}, errors.New("delivery must be steer or queue")
	}
	k.ensureExec()
	local, ok := k.execSnapshot().(exec.Continuable)
	if !ok {
		return exec.Task{}, exec.ErrNotContinuable
	}
	return local.Message(ctx, id, exec.Control{Kind: delivery, Text: text, Sender: sender})
}
func (k *Kernel) StopSubagent(ctx context.Context, id string) (exec.Task, error) {
	if !execID(id) {
		return exec.Task{}, exec.ErrNotFound
	}
	k.ensureExec()
	local, ok := k.execSnapshot().(*exec.Local)
	if !ok {
		return exec.Task{}, exec.ErrNotContinuable
	}
	return local.Stop(ctx, id)
}

// completedSubagentHistory projects only the parent's finished turns. In
// particular a final assistant followed by an owner interjection is still part
// of the current Handle and must not become a fork boundary.
func (k *Kernel) completedSubagentHistory(ctx context.Context) ([]provider.Message, error) {
	opening := turnSeqOf(ctx)
	if opening <= 0 {
		return nil, errors.New("dispatch: fork requires the current turn boundary")
	}
	cutoff := opening - 1
	st, err := k.currentState()
	if err != nil {
		return nil, err
	}
	st = migrate(st)
	blocks := make([]bot.SummaryBlock, 0, len(st.Blocks))
	for _, b := range st.Blocks {
		if b.To <= cutoff {
			blocks = append(blocks, b)
		}
	}
	var lines []bot.SeqEntry
	err = k.withCurrentIO(func(current *bot.Bot, _ *inbox.Inbox) error {
		var err error
		lines, err = current.UncoveredLines(blocks)
		return err
	})
	if err != nil {
		return nil, err
	}
	var raw []bot.Entry
	var seqs []int
	for _, ln := range lines {
		if ln.Seq > cutoff {
			break
		}
		e := ln.Entry
		e.Message.SetText(k.withLineNote(e.Message.Role, e.Message.Text(), e.Images, e.Audios))
		raw = append(raw, e)
		seqs = append(seqs, ln.Seq)
	}
	// No parent system messages or model overlay: the child has its own prefix.
	// projected is the existing tool-pair / branch projection.
	return projected(blocks, raw, seqs), nil
}

type agentMessageTool struct{ k *Kernel }

func (agentMessageTool) Name() string { return "agent_message" }
func (agentMessageTool) Description() string {
	return "Send a follow-up to your existing child, including a completed child. Default steer applies before its next step; queue starts another turn after this one finishes. Accepted messages are delivered without waiting for completion."
}
func (agentMessageTool) Parameters() any {
	return schema(map[string]any{"id": stringProp("Child id"), "text": stringProp("Follow-up"), "delivery": map[string]any{"type": "string", "enum": []string{"steer", "queue"}}}, []string{"id", "text"})
}
func (agentMessageTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "agent_message", nil
}
func (t agentMessageTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		ID       string `json:"id"`
		Text     string `json:"text"`
		Delivery string `json:"delivery"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	task, err := t.k.messageSubagent(ctx, in.ID, in.Text, in.Delivery, "bot:"+t.k.currentName())
	if err != nil {
		return "", err
	}
	return subagentReceipt(task)
}

type agentStopTool struct{ k *Kernel }

func (agentStopTool) Name() string { return "agent_stop" }
func (agentStopTool) Description() string {
	return "Stop a child's current work and discard earlier unconsumed messages. Its conversation remains available for an explicit new follow-up."
}
func (agentStopTool) Parameters() any {
	return schema(map[string]any{"id": stringProp("Child id")}, []string{"id"})
}
func (agentStopTool) Confirm(json.RawMessage) (bool, string, error) { return false, "agent_stop", nil }
func (t agentStopTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	task, err := t.k.StopSubagent(ctx, in.ID)
	if err != nil {
		return "", err
	}
	return subagentReceipt(task)
}
func subagentReceipt(task exec.Task) (string, error) {
	raw, err := json.Marshal(task)
	return string(raw), err
}

type sendParentTool struct {
	k  *Kernel
	id string
}

func (sendParentTool) Name() string { return "send_message_to_parent" }
func (sendParentTool) Description() string {
	return "Tell your parent Bot a finding or question before completion. Your final result is also delivered automatically. The runtime identifies you; there is no recipient parameter."
}
func (sendParentTool) Parameters() any {
	return schema(map[string]any{"text": stringProp("Finding or question for your parent Bot")}, []string{"text"})
}
func (sendParentTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "send_message_to_parent", nil
}
func (t sendParentTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	in.Text = strings.TrimSpace(in.Text)
	if in.Text == "" || len(in.Text) > 16<<10 {
		return "", errors.New("message must contain 1–16384 bytes")
	}
	local, ok := t.k.execSnapshot().(exec.Continuable)
	if !ok {
		return "", exec.ErrNotContinuable
	}
	ev, err := local.SendParent(ctx, t.id, in.Text)
	if err != nil {
		return "", err
	}
	return "sent to parent: " + ev.ID, nil
}

// continuableTools rebuilds mounted tools, but applies the child's original
// type policy and current model capabilities. Parent search/vision filtering
// must not remove a tool before this route can make its own choice.
func (k *Kernel) continuableTools(req exec.StartRequest) []tools.Tool {
	d := *req.Descriptor
	ty := agenttype.Type{Tools: d.Tools}
	base := k.scopeTools(req.ID)
	seen := map[string]bool{}
	for _, t := range base {
		seen[t.Name()] = true
	}
	mounted, _ := k.mountedTools() // keep the scoped tools if a lazy mount is unavailable
	for _, t := range mounted {
		if !seen[t.Name()] {
			seen[t.Name()] = true
			base = append(base, t)
		}
	}
	base = append(base, sendParentTool{k: k, id: req.ID})
	searchOn := true
	web, err := config.LoadWeb(k.Bot.Root)
	if err != nil || !web.SearchOn() {
		searchOn = false
	}
	model, vision := k.vision()
	if d.Provider != "" {
		if p, spec, err := config.Lookup(k.Bot.Root, d.Provider, d.Model); err == nil {
			model, vision = spec.Name, spec.SeesImages()
			if web.HostedSearchOn() && spec.HasHostedSearch(p.WireProtocol()) {
				searchOn = false
			}
		}
	}
	var out []tools.Tool
	for _, t := range base {
		switch t.Name() {
		case "dispatch", "spawn", "agent_message", "agent_stop", "steer", "queue", "interrupt", "cancel", "send_message_to_user":
			continue
		case "web_search":
			if !searchOn {
				continue
			}
		}
		if image, ok := t.(*tools.ViewImage); ok {
			// scopeTools created this instance for this refresh; mounted copies
			// were excluded by name. Do not copy its root binding mutex.
			image.Model = model
			image.Vision = vision
		}
		if ty.Allows(t.Name()) {
			out = append(out, t)
			continue
		}
		if review.IsShellTool(t.Name()) && ty.Allows(agenttype.ShellReadOnly) {
			out = append(out, readOnlyShell{Tool: t, k: k})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// subagentBudget is request-local. It never changes the parent's window,
// compactor, provider, calibration or context snapshots.
type subagentBudget struct {
	target, ceiling int
	ratio           float64
	compactor       compact.Compactor
}
