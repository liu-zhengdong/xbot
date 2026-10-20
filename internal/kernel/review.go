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
	"xbot/internal/bot"
	"xbot/internal/config"
	"xbot/internal/machine"
	"xbot/internal/review"
	"xbot/internal/tools"
)

// ErrApprovalExpired marks a confirmation that timed out or was cancelled
// before the owner decided. The record says "expired", and the wrapped error
// reaching the model says nobody confirmed, so a timeout never reads as a
// refusal by the owner.
var ErrApprovalExpired = errors.New("expired, no decision")

// reviewedTool is the single decision point before a model tool runs. It reads
// the concrete tool name and arguments and applies the one policy table in
// internal/review, plus the machine switch and the persona/profile guard. A
// tool's own Confirm never decides; it only supplies the card summary.
type reviewedTool struct {
	tools.Tool
	k *Kernel
}

func (t reviewedTool) Confirm(args json.RawMessage) (bool, string, error) {
	return t.ConfirmContext(context.Background(), args)
}

// ConfirmContext decides against the caller's working directory. File tools
// and shell on one link are shared by the foreground and every background
// branch, so the directory comes from ctx, not from a kernel-global.
func (t reviewedTool) ConfirmContext(ctx context.Context, args json.RawMessage) (bool, string, error) {
	// A call the foreground will refuse is refused before any card: the owner
	// is never asked to approve something that cannot run.
	if t.k != nil {
		if err := t.k.foregroundRefuses(ctx, t.Tool, args); err != nil {
			return false, "", err
		}
	}
	if t.k != nil && t.k.deniedBefore(t.Name(), args) {
		return false, "", errors.New(msgDenied)
	}
	_, summary, err := tools.ConfirmCall(ctx, t.Tool, args)
	if err != nil {
		return false, "", err
	}
	return t.k.reviewCall(ctx, t.Tool, args, summary)
}

// Run applies file freshness, then the background conflict queue. A foreground
// write to something a running background has written is queued behind that
// background instead of running beside it.
func (t reviewedTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	name := t.Name()
	if err := t.k.foregroundRefuses(ctx, t.Tool, args); err != nil {
		return "", err
	}
	if name == "read_file" {
		if s, ok := t.k.unchangedReceipt(ctx, t.Tool, args); ok {
			return s, nil
		}
	}
	if isFileEdit(name) {
		if path, dir, _, _ := t.k.memoryCall(ctx, t.Tool, args); dir != "" {
			if err := memorySymlinks(dir, path); err != nil {
				return "", err
			}
		}
		if err := t.k.rejectUnreadOrStale(ctx, t.Tool, args); err != nil {
			return "", err
		}
	}
	start := time.Now()
	out, ran, err := t.execute(ctx, args)
	if err != nil || !ran {
		return out, err
	}
	switch name {
	case "shell":
		// A command moved to the background may still be writing; its
		// changes are reported as usual.
		if !strings.HasPrefix(out, "background ") {
			if changed := t.k.noteShellWrites(ctx, start, time.Now()); len(changed) > 0 {
				out = appendNote(out, "files you had read were changed by this command (records refreshed; read them again to see the new bytes): "+strings.Join(changed, ", "))
			}
		}
	case "read_file":
		t.k.noteRead(ctx, t.Tool, args, out)
	case "write_file", "edit_file":
		if t.k.noteWrite(ctx, t.Tool, args) {
			out = appendNote(out, msgRefresh)
		}
	}
	return out, nil
}

// execute runs the tool, or queues it behind a conflicting background.
// ran is false when the call was queued and did not touch the file.
func (t reviewedTool) execute(ctx context.Context, args json.RawMessage) (string, bool, error) {
	k := t.k
	base := k.WorkDir
	if dir := k.cwdFor(ctx); dir != "" {
		base = dir
	}
	if res, ok := review.ResourceOf(t.Tool.Name(), args, base); ok {
		cur := k.turnFor(ctx)
		if !cur.isForked() {
			if bg := k.conflictingBackground(cur, res); bg != nil {
				_, summary, _ := tools.ConfirmCall(ctx, t.Tool, args)
				if k.QueueFull() {
					return fmt.Sprintf("队列已满（%d 个），没有建任务：这一步和后台正在动的 %s 冲突，等它做完再试。", QueueCap, res), false, nil
				}
				c := &Control{ID: fmt.Sprintf("q%d", ctrlSeq.Add(1)), Kind: "conflict", Resource: res, Text: t.Tool.Name() + " " + summary}
				if bg.queueCtrl(c) {
					return fmt.Sprintf("已排到后台 %s 之后（队列 %s）：它正在动 %s，做完当前的事会接着做这一步。", c.Background, c.ID, res), false, nil
				}
			}
		}
		cur.addResource(res)
	}
	out, err := t.runMemoryGuard(ctx, args)
	return out, true, err
}

// reviewCall returns whether the owner must confirm this concrete call.
func (k *Kernel) reviewCall(ctx context.Context, tool tools.Tool, args json.RawMessage, summary string) (bool, string, error) {
	name := tool.Name()
	// The machine switch is orthogonal to the class: it applies to every tool
	// that touches this machine. off refuses, ask confirms each action, and
	// allow falls through to the table.
	if review.MachineTouching(name) {
		mode, err := k.machineMode()
		if err != nil {
			return false, "", err
		}
		switch mode {
		case machine.Off:
			return false, "", errors.New("machine local is off")
		case machine.Ask:
			return true, k.protectedSummary(ctx, tool, args, summary), nil
		}
	}
	// A call that reaches a vault or its key always asks, even in allow mode:
	// the vault is the one thing a bot's own shell should not read.
	if k.protectedCall(ctx, tool, args) {
		return true, summary + review.ProtectedSuffix, nil
	}
	if isFileEdit(name) && !k.turnFor(ctx).turnTrusted {
		if _, dir, _, _ := k.memoryCall(ctx, tool, args); dir != "" {
			return true, summary + " (untrusted turn: memory write)", nil
		}
	}
	if name == "shell" && !k.turnFor(ctx).turnTrusted && k.Bot != nil && !review.ShellIsReadOnly(args) {
		root := k.Bot.Root
		memoryDirs := []string{filepath.Join(root, "bots", k.Bot.Name(), "memory")}
		if person := k.memoryPersonOf(k.turnFor(ctx)); person != "" {
			memoryDirs = append(memoryDirs, filepath.Join(root, "people", person, "memory"))
		}
		for _, kind := range []string{"bots", "people"} {
			entries, _ := os.ReadDir(filepath.Join(root, kind))
			for _, entry := range entries {
				memoryDirs = append(memoryDirs, filepath.Join(root, kind, entry.Name(), "memory"))
			}
		}
		cmd, cwd := review.ShellInvocation(args)
		if cwd == "" {
			cwd = k.cwdFor(ctx)
		}
		if review.TouchesProtected(cmd, cwd, memoryDirs) || review.MentionsMemory(cmd) {
			return true, summary + " (untrusted turn: memory access)", nil
		}
	}
	// The owner turned the table's cards off (ADR 0019): the checks above
	// stay (an explicit machine setting, the vault, a turn from outside
	// touching memory), and so does the links card, which is where the
	// owner types a connector's keys.
	if k != nil && k.Permissive && name != "links" {
		return false, summary, nil
	}
	if review.ClassOf(name) == review.SelfEdit && isFileEdit(name) {
		// write_file and edit_file are a self-edit only when the resolved path
		// is this bot's own persona or profile file.
		return k.selfEditPath(ctx, tool, args), summary, nil
	}
	if h, ok := tool.(review.ReadOnlyHinter); ok && review.ClassOfMCP(name, h.ReadOnlyHint()) == review.Readonly {
		return false, summary, nil
	}
	return review.NeedsOwner(name, args), summary, nil
}

// callPath is the absolute path a file tool would use for this context.
func callPath(ctx context.Context, tool tools.Tool, args json.RawMessage) string {
	if p, ok := tool.(tools.PathContext); ok {
		return p.ResolvedPathContext(ctx, args)
	}
	pt, ok := tool.(tools.PathTool)
	if !ok {
		return ""
	}
	return pt.ResolvedPath(args)
}

// protectedPaths are the paths no bot action may touch without a card: this
// bot's vault directory, the shared vault keys directory, every bot's vault
// directory under the data root, the root web.json and providers.json
// (those files name API key variables), and the root env file, which holds key
// values and endpoints the desktop app loads at launch.
func (k *Kernel) protectedPaths() []string {
	if k == nil || k.Bot == nil || k.Bot.Root == "" {
		return nil
	}
	root := k.Bot.Root
	out := []string{k.Bot.VaultDir(), bot.VaultKeysDir(root), config.WebPath(root), config.Path(root), filepath.Join(root, "env"), filepath.Join(root, "keys", "remote"), filepath.Join(root, "remote")}
	botsDir := filepath.Join(root, "bots")
	entries, err := os.ReadDir(botsDir)
	if err != nil {
		return out
	}
	for _, entry := range entries {
		if entry.IsDir() {
			out = append(out, filepath.Join(botsDir, entry.Name(), "vault"))
			out = append(out, filepath.Join(botsDir, entry.Name(), "secrets.json"))
		}
	}
	return out
}

// protectedCall reports whether a file tool's resolved path or a shell command
// touches a protected path.
func (k *Kernel) protectedCall(ctx context.Context, tool tools.Tool, args json.RawMessage) bool {
	name := tool.Name()
	if !review.IsFileTool(name) && name != "shell" {
		return false
	}
	protected := k.protectedPaths()
	if len(protected) == 0 {
		return false
	}
	if name == "shell" {
		cmd, cwd := review.ShellInvocation(args)
		if cwd == "" {
			cwd = k.cwdFor(ctx)
		}
		return review.TouchesProtected(cmd, cwd, protected) || review.RemoteCLIWrites(cmd)
	}
	path := callPath(ctx, tool, args)
	if path == "" {
		return false
	}
	return review.TouchesProtected(path, "", protected)
}

// protectedSummary appends the vault note to a card summary only when the call
// touches a protected path.
func (k *Kernel) protectedSummary(ctx context.Context, tool tools.Tool, args json.RawMessage, summary string) string {
	if !k.protectedCall(ctx, tool, args) {
		return summary
	}
	return summary + review.ProtectedSuffix
}

// machineMode reads the local machine switch. A nil kernel or bot is allow, so
// unit tests and older processes keep the table behavior.
func (k *Kernel) machineMode() (string, error) {
	if k == nil {
		return machine.Allow, nil
	}
	if k.Machine != nil {
		return k.Machine()
	}
	if k.Bot == nil || k.Bot.Root == "" {
		return machine.Allow, nil
	}
	return machine.Get(k.Bot.Root, machine.Local)
}

func isFileEdit(name string) bool {
	return name == "write_file" || name == "edit_file"
}

// selfEditPath reports whether the resolved path of a file tool is this bot's
// own persona or profile file. A tool that cannot report a path is a self-edit
// so an unknown path never slips past the guard.
func (k *Kernel) selfEditPath(ctx context.Context, tool tools.Tool, args json.RawMessage) bool {
	if _, ok := tool.(tools.PathTool); !ok {
		if _, ok := tool.(tools.PathContext); !ok {
			return true
		}
	}
	path := callPath(ctx, tool, args)
	if path == "" {
		return true
	}
	guard := k.selfEditGuard()
	return guard == nil || guard(path)
}

// reviewedTools wraps every tool the kernel hands to the model.
func (k *Kernel) reviewedTools(list []tools.Tool) []tools.Tool {
	out := make([]tools.Tool, len(list))
	for i, t := range list {
		if t == nil {
			continue
		}
		out[i] = reviewedTool{Tool: t, k: k}
	}
	return out
}

// trackApprove records the owner's decision after the wrapped approver answers.
func (k *Kernel) trackApprove(fn agent.Approver) agent.Approver {
	return func(ctx context.Context, tool, summary string) (bool, error) {
		approved, err := fn(ctx, tool, summary)
		switch {
		case errors.Is(err, ErrApprovalExpired):
			// Nobody answered: the card is neither approved nor declined, and
			// the tool must say so instead of blaming the owner.
			k.RecordDecision(bot.DecisionExpired, tool, summary)
			return false, fmt.Errorf("%w: %s", ErrApprovalExpired, tool)
		case err != nil:
			return false, err
		}
		state := bot.DecisionDeclined
		if approved {
			state = bot.DecisionAllowed
		} else {
			k.noteDenial(tool, agent.ToolArgs(ctx))
		}
		k.RecordDecision(state, tool, summary)
		return approved, nil
	}
}

// RecordDecision appends one review outcome to this bot's decisions.jsonl.
// The summary is redacted before it is written, so no credential value lands
// in the log.
func (k *Kernel) RecordDecision(state, tool, summary string) {
	if k == nil || k.Bot == nil {
		return
	}
	summary = k.redactor()(summary)
	summary = scrubSecrets(summary, k.secretValues())
	_ = k.Bot.LogDecision(state, tool, summary, k.now())
}
