package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"xbot/internal/agent"
	"xbot/internal/cliops"
	"xbot/internal/review"
	"xbot/internal/tools"
)

type routineCallSourceKey struct{}
type routineChildPolicyKey struct{}

// This belongs to one real source Shell call, not a current-turn lookup.
type routineCallSource struct {
	listener *cliops.Server
	owner    *turn
	child    bool
	trusted  bool
	person   string
}

func (k *Kernel) cliCallEnvironment(ctx context.Context, base []string) []string {
	out := k.CLIEnvironment(base)
	source, ok := ctx.Value(routineCallSourceKey{}).(routineCallSource)
	if !ok || source.listener == nil {
		return out
	}
	k.operations.mu.Lock()
	active := k.operations.server == source.listener
	k.operations.mu.Unlock()
	if id := tools.CallID(ctx); active && id != "" {
		out = append(out, "XBOT_CALL_ID="+id)
	}
	return out
}

// reviewIdentity preserves the old policy for ordinary calls. A source child
// has its own explicit untrusted identity, never a fallback to its parent's turn.
func (k *Kernel) reviewIdentity(ctx context.Context) (bool, string) {
	if source, ok := ctx.Value(routineCallSourceKey{}).(routineCallSource); ok {
		return source.trusted, source.person
	}
	t := k.turnFor(ctx)
	return t.turnTrusted, k.memoryPersonOf(t)
}

// checkRuntimeRoutinePrecheck connects only the existing real source Job to the
// original agent call's policy and wrapped approver. Timer methods hold no locks
// while this runs. Ordinary CRUD with no effective command needs no bridge.
func (k *Kernel) checkRuntimeRoutinePrecheck(ctx context.Context, command string, intent routineReviewRequest) error {
	if strings.TrimSpace(command) == "" {
		return nil
	}
	unavailable := errors.New("routine: source approval context unavailable")
	id := cliops.RoutineCallID(ctx)
	if id == "" {
		return unavailable
	}
	original, ok := k.liveShell().CallContext(id)
	if !ok {
		return unavailable
	}
	source, ok := original.Value(routineCallSourceKey{}).(routineCallSource)
	if !ok || source.listener == nil || source.owner == nil && !source.child {
		return unavailable
	}
	k.operations.mu.Lock()
	active := k.operations.server == source.listener
	k.operations.mu.Unlock()
	if !active {
		return unavailable
	}
	name, approve := agent.ToolApprover(original)
	if !review.IsShellTool(name) {
		return unavailable
	}
	callCtx, cancel := context.WithCancel(original)
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	defer cancel()
	if err := callCtx.Err(); err != nil {
		return err
	}

	// Park/grant and denial keys include the actual decoded request and retained
	// command. A different file body must not reuse approval of the same path.
	var args map[string]json.RawMessage
	if err := json.Unmarshal(agent.ToolArgs(original), &args); err != nil || args == nil {
		return unavailable
	}
	detail := struct {
		Action            string                `json:"action"`
		RequestID         string                `json:"request_id"`
		Create            *cliops.RoutineCreate `json:"create,omitempty"`
		Patch             *cliops.RoutinePatch  `json:"patch,omitempty"`
		EffectivePrecheck string                `json:"effective_precheck"`
	}{intent.Action, intent.RequestID, intent.Create, intent.Patch, command}
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	args["_xbot_routine"] = raw
	approvalArgs, err := json.Marshal(args)
	if err != nil {
		return err
	}
	if k.deniedBefore(name, approvalArgs) {
		return errors.New("not run: this call was already declined in this turn")
	}
	originalCommand, _ := review.ShellInvocation(agent.ToolArgs(original))
	summary := fmt.Sprintf("%s routine %s; repeat the original CLI command: %s", intent.Action, intent.RequestID, originalCommand)
	need, summary, err := k.reviewPrecheck(callCtx, command, summary)
	if err != nil {
		return err
	}
	if need {
		if approve == nil {
			return noApprover{}
		}
		// The policy uses frozen identity above; the card follows the original
		// owner's actual fork branch. Children have no borrowed main owner.
		owner := source.owner
		if source.child {
			owner = nil
		}
		approvalCtx := context.WithValue(callCtx, turnKey{}, owner)
		approved, err := approve(approvalCtx, approvalArgs, summary)
		if err != nil {
			if result, ok := err.(interface{ ToolResult() string }); ok {
				return errors.New(result.ToolResult())
			}
			return err
		}
		if !approved {
			return errors.New("denied by user")
		}
	}
	return callCtx.Err()
}
