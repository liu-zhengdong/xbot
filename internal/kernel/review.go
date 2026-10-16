package kernel

import (
	"context"
	"encoding/json"
	"errors"

	"xbot/internal/agent"
	"xbot/internal/bot"
	"xbot/internal/review"
	"xbot/internal/tools"
)

// ErrApprovalExpired marks a confirmation that timed out or was cancelled
// before the owner decided. trackApprove records it as expired and reports a
// plain "no" so the tool loop does not treat the timeout as a hard error.
var ErrApprovalExpired = errors.New("approval expired")

// reviewedTool enforces the kernel's dangerous-operation policy in front of a
// tool's own Confirm. It reads the concrete tool name and arguments, so a tool
// cannot skip review by returning need=false.
type reviewedTool struct{ tools.Tool }

func (t reviewedTool) Confirm(args json.RawMessage) (bool, string, error) {
	need, summary, err := t.Tool.Confirm(args)
	if err != nil {
		return false, "", err
	}
	if forced, _ := review.Check(t.Tool.Name(), args); forced {
		return true, summary, nil
	}
	return need, summary, nil
}

// reviewedTools wraps every tool the kernel hands to the model.
func reviewedTools(list []tools.Tool) []tools.Tool {
	out := make([]tools.Tool, len(list))
	for i, t := range list {
		if t == nil {
			continue
		}
		out[i] = reviewedTool{t}
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
