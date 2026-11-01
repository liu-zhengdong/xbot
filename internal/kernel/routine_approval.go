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
)

// Timer methods hold no locks while the existing source policy and approver run.
// Ordinary CRUD with no effective command needs no original-call bridge.
func (k *Kernel) checkRuntimeRoutinePrecheck(ctx context.Context, command string, intent routineReviewRequest) error {
	if strings.TrimSpace(command) == "" {
		return nil
	}
	callCtx, cleanup, err := k.runtimeApprovalSource(ctx)
	if err != nil {
		return fmt.Errorf("routine: %w", err)
	}
	defer cleanup()
	var args map[string]json.RawMessage
	if err := json.Unmarshal(agent.ToolArgs(callCtx), &args); err != nil || args == nil {
		return errors.New("routine: source approval context unavailable")
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
	originalCommand, _ := review.ShellInvocation(agent.ToolArgs(callCtx))
	summary := fmt.Sprintf("%s routine %s; repeat the original CLI command: %s", intent.Action, intent.RequestID, originalCommand)
	need, summary, err := k.reviewPrecheck(callCtx, command, summary)
	if err != nil {
		return err
	}
	return k.approveRuntimeIntent(callCtx, approvalArgs, summary, need)
}
