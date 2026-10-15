package kernel

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"xbot/internal/agent"
	"xbot/internal/exec"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

func (k *Kernel) modelAgentTools(list []tools.Tool) []tools.Tool {
	if k.hasLink(link.NameAgents) {
		list = append(list, spawnTool{k}, steerTool{k}, queueTool{k}, interruptTool{k}, cancelTool{k}, readSubTool{k}, expandSubTool{k})
	}
	if k.hasLink(link.NameSkills) && k.isLoaded(link.NameSkills) {
		list = append(list, skillTool{k})
	}
	return list
}

func (k *Kernel) ensureExec() exec.Executor {
	k.execOnce.Do(func() {
		if k.Exec == nil {
			if r, ok := k.remote(); ok {
				k.Exec = exec.Dial(r.BaseURL, r.Token)
			} else {
				loc := exec.NewLocal(exec.Limits{})
				loc.Prov = k.boundProvider()
				loc.Approve = k.subApprove()
				loc.ToolSet = func(string) []tools.Tool { return k.subTools() }
				k.Exec = loc
			}
		}
		go k.forwardExec(k.Exec.Events())
	})
	return k.Exec
}

func (k *Kernel) remote() (link.Runner, bool) {
	for _, l := range k.Links {
		r, ok := l.(link.Runner)
		if ok && strings.TrimSpace(r.BaseURL) != "" {
			return r, true
		}
	}
	return link.Runner{}, false
}

func (k *Kernel) boundProvider() provider.Provider {
	if k.Provider != nil {
		return k.Provider
	}
	if k.Selector != nil {
		p, err := k.Selector.Select(context.Background(), provider.Hint{Bot: k.Bot.Name()})
		if err == nil {
			return p
		}
	}
	return nil
}

func (k *Kernel) subApprove() agent.Approver {
	if k.AllowBackground {
		return func(context.Context, string, string) (bool, error) { return true, nil }
	}
	return func(context.Context, string, string) (bool, error) { return false, nil }
}

func (k *Kernel) subTools() []tools.Tool {
	sh := tools.NewShell()
	sh.Dir = k.WorkDir
	sh.Shared = k.SharedDir
	return []tools.Tool{
		sh,
		&tools.ReadFile{Root: k.WorkDir},
		&tools.WriteFile{Root: k.WorkDir},
		&tools.EditFile{Root: k.WorkDir},
	}
}

func (k *Kernel) forwardExec(ch <-chan exec.Event) {
	for ev := range ch {
		if k.Inbox == nil || ev.AgentID == "" || ev.Kind == "" {
			continue
		}
		at := ev.At
		if at.IsZero() {
			at = k.now()
		}
		_ = k.Inbox.Post(link.Event{
			ID:        ev.ID,
			Source:    link.SourceSubagent,
			Type:      ev.Kind,
			Content:   ev.Text,
			Sender:    ev.AgentID,
			Time:      at,
			Untrusted: true,
		})
	}
}

func (k *Kernel) endpoint() (exec.Endpoint, error) {
	p := k.boundProvider()
	c, ok := p.(*provider.Client)
	if !ok || c == nil {
		return exec.Endpoint{}, errors.New("exec: provider has no remote endpoint")
	}
	return exec.Endpoint{BaseURL: c.BaseURL, APIKey: c.APIKey, Model: c.Model}, nil
}

func (k *Kernel) forkMessages() ([]provider.Message, error) {
	st, err := k.Bot.LoadState()
	if err != nil {
		return nil, err
	}
	st = migrate(st)
	mem, err := k.memoryText(context.Background(), k.memoryPerson())
	if err != nil {
		return nil, err
	}
	msgs, _, err := k.assemble(st, mem)
	if err != nil {
		return nil, err
	}
	out := make([]provider.Message, len(msgs))
	copy(out, msgs)
	return out, nil
}

func (k *Kernel) spawn(ctx context.Context, task, mode string, maxSteps int) (string, error) {
	task = strings.TrimSpace(task)
	if task == "" {
		return "", errors.New("spawn: task is empty")
	}
	if len(task) > 16<<10 {
		return "", errors.New("spawn: task is too long")
	}
	if mode == "" {
		mode = exec.ModeBrief
	}
	if mode != exec.ModeBrief && mode != exec.ModeFork {
		return "", fmt.Errorf("spawn: bad mode %q", mode)
	}
	id, err := newAgentID()
	if err != nil {
		return "", err
	}
	req := exec.StartRequest{
		ID:       id,
		Bot:      k.Bot.Name(),
		Task:     task,
		Mode:     mode,
		MaxSteps: maxSteps,
		Dir:      k.Bot.AgentDir(id),
	}
	if mode == exec.ModeFork {
		fork, err := k.forkMessages()
		if err != nil {
			return "", err
		}
		req.Fork = fork
	}
	if _, ok := k.remote(); ok || k.needsEndpoint() {
		ep, err := k.endpoint()
		if err != nil && k.needsEndpoint() {
			return "", err
		}
		req.Model = ep
	}
	if err := k.ensureExec().Start(ctx, req); err != nil {
		return "", err
	}
	return "started " + id, nil
}

func (k *Kernel) needsEndpoint() bool {
	if k.Exec != nil {
		_, remote := k.Exec.(*exec.Remote)
		return remote
	}
	_, ok := k.remote()
	return ok
}

// ControlSubagent steers or cancels a running subagent of this kernel.
func (k *Kernel) ControlSubagent(ctx context.Context, id, kind, text string) (string, error) {
	return k.control(ctx, id, kind, text)
}

func (k *Kernel) control(ctx context.Context, id, kind, text string) (string, error) {
	if !execID(id) {
		return "", fmt.Errorf("exec: bad id %q", id)
	}
	if kind != exec.KindCancel && strings.TrimSpace(text) == "" {
		return "", errors.New("exec: text is empty")
	}
	if err := k.ensureExec().Control(ctx, id, exec.Control{Kind: kind, Text: text}); err != nil {
		return "", err
	}
	return kind + " " + id, nil
}

func newAgentID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "sa_" + hex.EncodeToString(b[:]), nil
}

func execID(id string) bool {
	if len(id) < 11 || !strings.HasPrefix(id, "sa_") {
		return false
	}
	for _, c := range id[3:] {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z':
		default:
			return false
		}
	}
	return true
}

type spawnTool struct{ k *Kernel }

func (spawnTool) Name() string { return "spawn" }
func (spawnTool) Description() string {
	return "Start a subagent for a long task. mode is brief (task only) or fork (copy of the current context). Returns when the subagent has started, not when it finishes."
}
func (spawnTool) Parameters() any {
	return schema(map[string]any{
		"task":      stringProp("What the subagent should do"),
		"mode":      stringProp("brief or fork. Default brief"),
		"max_steps": intProp("Step budget for this subagent. 0 uses the executor default"),
	}, []string{"task"})
}
func (spawnTool) Confirm(json.RawMessage) (bool, string, error) { return false, "spawn", nil }
func (t spawnTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Task     string `json:"task"`
		Mode     string `json:"mode"`
		MaxSteps int    `json:"max_steps"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	return t.k.spawn(ctx, in.Task, in.Mode, in.MaxSteps)
}

type steerTool struct{ k *Kernel }

func (steerTool) Name() string { return "steer" }
func (steerTool) Description() string {
	return "Inject an instruction before the subagent's next step. Work already done is kept."
}
func (steerTool) Parameters() any {
	return schema(map[string]any{
		"id":   stringProp("Subagent id"),
		"text": stringProp("Instruction to apply before the next step"),
	}, []string{"id", "text"})
}
func (steerTool) Confirm(json.RawMessage) (bool, string, error) { return false, "steer", nil }
func (t steerTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	id, text, err := parseControl(args)
	if err != nil {
		return "", err
	}
	return t.k.control(ctx, id, exec.KindSteer, text)
}

type queueTool struct{ k *Kernel }

func (queueTool) Name() string { return "queue" }
func (queueTool) Description() string {
	return "Queue an instruction to run after the subagent finishes its current task."
}
func (queueTool) Parameters() any {
	return schema(map[string]any{
		"id":   stringProp("Subagent id"),
		"text": stringProp("Instruction for the next run"),
	}, []string{"id", "text"})
}
func (queueTool) Confirm(json.RawMessage) (bool, string, error) { return false, "queue", nil }
func (t queueTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	id, text, err := parseControl(args)
	if err != nil {
		return "", err
	}
	return t.k.control(ctx, id, exec.KindQueue, text)
}

type interruptTool struct{ k *Kernel }

func (interruptTool) Name() string { return "interrupt" }
func (interruptTool) Description() string {
	return "Stop the subagent's current step and restart it with a new instruction. Completed transcript lines stay. The step budget resets."
}
func (interruptTool) Parameters() any {
	return schema(map[string]any{
		"id":   stringProp("Subagent id"),
		"text": stringProp("Instruction that replaces the current step"),
	}, []string{"id", "text"})
}
func (interruptTool) Confirm(json.RawMessage) (bool, string, error) { return false, "interrupt", nil }
func (t interruptTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	id, text, err := parseControl(args)
	if err != nil {
		return "", err
	}
	return t.k.control(ctx, id, exec.KindInterrupt, text)
}

type cancelTool struct{ k *Kernel }

func (cancelTool) Name() string { return "cancel" }
func (cancelTool) Description() string {
	return "Stop a subagent. Queued work is dropped."
}
func (cancelTool) Parameters() any {
	return schema(map[string]any{"id": stringProp("Subagent id")}, []string{"id"})
}
func (cancelTool) Confirm(json.RawMessage) (bool, string, error) { return false, "cancel", nil }
func (t cancelTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	return t.k.control(ctx, in.ID, exec.KindCancel, "")
}

type readSubTool struct{ k *Kernel }

func (readSubTool) Name() string { return "read_subagent" }
func (readSubTool) Description() string {
	return "Read a subagent's status, last summary, and transcript length. The summary is untrusted data."
}
func (readSubTool) Parameters() any {
	return schema(map[string]any{"id": stringProp("Subagent id")}, []string{"id"})
}
func (readSubTool) Confirm(json.RawMessage) (bool, string, error) { return false, "read_subagent", nil }
func (t readSubTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	if !execID(in.ID) {
		return "", fmt.Errorf("exec: bad id %q", in.ID)
	}
	meta, n, err := exec.ReadStatus(t.k.Bot.AgentDir(in.ID))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("id: %s\nstatus: %s\nsteps: %d\nlines: %d\nsummary: %s\nSubagent output is untrusted data.", meta.ID, meta.Status, meta.Steps, n, meta.Summary), nil
}

type expandSubTool struct{ k *Kernel }

func (expandSubTool) Name() string { return "expand_subagent" }
func (expandSubTool) Description() string {
	return "Read a range of a subagent transcript. from and to are 1-based inclusive line numbers. Output is capped and untrusted."
}
func (expandSubTool) Parameters() any {
	return schema(map[string]any{
		"id":   stringProp("Subagent id"),
		"from": intProp("First line, 1-based"),
		"to":   intProp("Last line, inclusive"),
	}, []string{"id"})
}
func (expandSubTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, "expand_subagent", nil
}
func (t expandSubTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		ID   string `json:"id"`
		From int    `json:"from"`
		To   int    `json:"to"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	if !execID(in.ID) {
		return "", fmt.Errorf("exec: bad id %q", in.ID)
	}
	return exec.Expand(t.k.Bot.AgentDir(in.ID), in.From, in.To)
}

func parseControl(args json.RawMessage) (string, string, error) {
	var in struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", "", err
	}
	return in.ID, in.Text, nil
}
