package kernel

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"xbot/internal/agent"
	"xbot/internal/review"
)

func (k *Kernel) checkRuntimePersona(ctx context.Context, textEffective string) error {
	// Persona keeps SelfEdit's unconditional strict confirmation, even after
	// the original model tool is retired from the permission table.
	if k.permissive() {
		return nil
	}
	callCtx, cleanup, err := k.runtimeApprovalSource(ctx)
	if err != nil {
		return fmt.Errorf("persona: %w", err)
	}
	defer cleanup()
	var args map[string]json.RawMessage
	if err := json.Unmarshal(agent.ToolArgs(callCtx), &args); err != nil || args == nil {
		return errors.New("persona: source approval context unavailable")
	}
	detail := struct {
		Action     string `json:"action"`
		TextSHA256 string `json:"text_sha256"`
		Bytes      int    `json:"bytes"`
	}{"set", fmt.Sprintf("%x", sha256.Sum256([]byte(textEffective))), len(textEffective)}
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	args["_xbot_persona"] = raw
	approvalArgs, err := json.Marshal(args)
	if err != nil {
		return err
	}
	command, _ := review.ShellInvocation(agent.ToolArgs(callCtx))
	return k.approveRuntimeIntent(callCtx, approvalArgs, fmt.Sprintf("set persona; repeat the original CLI command: %s", command), true)
}
