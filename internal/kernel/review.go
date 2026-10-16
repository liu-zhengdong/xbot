package kernel

import (
	"context"
	"encoding/json"
	"errors"

	"xbot/internal/agent"
	"xbot/internal/bot"
	"xbot/internal/machine"
	"xbot/internal/review"
	"xbot/internal/tools"
)

// ErrApprovalExpired marks a confirmation that timed out or was cancelled
// before the owner decided. trackApprove records it as expired and reports a
// plain "no" so the tool loop does not treat the timeout as a hard error.
var ErrApprovalExpired = errors.New("approval expired")

// reviewedTool is the single decision point before a model tool runs. It reads
// the concrete tool name and arguments and applies the one policy table in
// internal/review, plus the machine switch and the persona/profile guard. A
// tool's own Confirm never decides; it only supplies the card summary.
type reviewedTool struct {
	tools.Tool
	k *Kernel
}

func (t reviewedTool) Confirm(args json.RawMessage) (bool, string, error) {
	_, summary, err := t.Tool.Confirm(args)
	if err != nil {
		return false, "", err
	}
	return t.k.reviewCall(t.Tool, args, summary)
}

// reviewCall returns whether the owner must confirm this concrete call.
func (k *Kernel) reviewCall(tool tools.Tool, args json.RawMessage, summary string) (bool, string, error) {
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
			return true, summary, nil
		}
	}
	if review.ClassOf(name) == review.SelfEdit && isFileEdit(name) {
		// write_file and edit_file are a self-edit only when the resolved path
		// is this bot's own persona or profile file.
		return k.selfEditPath(tool, args), summary, nil
	}
	return review.NeedsOwner(name, args), summary, nil
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
func (k *Kernel) selfEditPath(tool tools.Tool, args json.RawMessage) bool {
	pt, ok := tool.(tools.PathTool)
	if !ok {
		return true
	}
	path := pt.ResolvedPath(args)
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
			k.RecordDecision(bot.DecisionExpired, tool, summary)
			return false, nil
		case err != nil:
			return false, err
		}
		state := bot.DecisionDeclined
		if approved {
			state = bot.DecisionAllowed
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
