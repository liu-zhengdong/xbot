package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"xbot/internal/agent"
	"xbot/internal/review"
)

func (k *Kernel) checkRuntimeBotDelete(ctx context.Context, name string) error {
	// Delete has the existing unconditional strict confirmation semantics. Its
	// retired tool name must not be looked up as an unknown/Normal permission.
	if k.permissive() {
		return nil
	}
	callCtx, cleanup, err := k.runtimeApprovalSource(ctx)
	if err != nil {
		return fmt.Errorf("bot: %w", err)
	}
	defer cleanup()
	var args map[string]json.RawMessage
	if err := json.Unmarshal(agent.ToolArgs(callCtx), &args); err != nil || args == nil {
		return errors.New("bot: source approval context unavailable")
	}
	detail := struct {
		Action string `json:"action"`
		Name   string `json:"name"`
	}{"delete", name}
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	args["_xbot_bot"] = raw
	approvalArgs, err := json.Marshal(args)
	if err != nil {
		return err
	}
	command, _ := review.ShellInvocation(agent.ToolArgs(callCtx))
	return k.approveRuntimeIntent(callCtx, approvalArgs, fmt.Sprintf("delete bot %s; repeat the original CLI command: %s", name, command), true)
}
