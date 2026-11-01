package kernel

import (
	"context"
	"encoding/json"
	"errors"

	"xbot/internal/agent"
	"xbot/internal/cliops"
	"xbot/internal/review"
	"xbot/internal/tools"
)

type routineCallSourceKey struct{}
type routineChildPolicyKey struct{}

// The existing source marker belongs to one real Shell call. Both runtime
// families use it; it is never a current-turn lookup or an additional registry.
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

// Policy identity stays frozen even when approval cards follow the original
// owner's current fork branch. Child calls have explicit untrusted/no-person.
func (k *Kernel) reviewIdentity(ctx context.Context) (bool, string) {
	if source, ok := ctx.Value(routineCallSourceKey{}).(routineCallSource); ok {
		return source.trusted, source.person
	}
	t := k.turnFor(ctx)
	return t.turnTrusted, k.memoryPersonOf(t)
}

// runtimeApprovalSource resolves the original real Shell call, never a current
// turn or disk record. It returns a card context with that call's frozen policy
// metadata and original owner's dynamic branch, and releases no caller locks.
func (k *Kernel) runtimeApprovalSource(ctx context.Context) (context.Context, func(), error) {
	unavailable := errors.New("source approval context unavailable")
	id := cliops.SourceCallID(ctx)
	if id == "" {
		return nil, nil, unavailable
	}
	original, ok := k.liveShell().CallContext(id)
	if !ok {
		return nil, nil, unavailable
	}
	source, ok := original.Value(routineCallSourceKey{}).(routineCallSource)
	if !ok || source.listener == nil || source.owner == nil && !source.child {
		return nil, nil, unavailable
	}
	k.operations.mu.Lock()
	active := k.operations.server == source.listener
	k.operations.mu.Unlock()
	name, _ := agent.ToolApprover(original)
	if !active || !review.IsShellTool(name) {
		return nil, nil, unavailable
	}
	callCtx, cancel := context.WithCancel(original)
	stop := context.AfterFunc(ctx, cancel)
	cleanup := func() { stop(); cancel() }
	if err := callCtx.Err(); err != nil {
		cleanup()
		return nil, nil, err
	}
	owner := source.owner
	if source.child {
		owner = nil
	}
	return context.WithValue(callCtx, turnKey{}, owner), cleanup, nil
}

// approveRuntimeIntent uses the original wrapped Shell approver and its existing
// denial/grant machinery. The caller supplies typed canonical approval-only args
// and its established policy decision; actual Shell.Run arguments never change.
func (k *Kernel) approveRuntimeIntent(ctx context.Context, args json.RawMessage, summary string, need bool) error {
	name, approve := agent.ToolApprover(ctx)
	if k.deniedBefore(name, args) {
		return errors.New("not run: this call was already declined in this turn")
	}
	if !need {
		return ctx.Err()
	}
	if approve == nil {
		return noApprover{}
	}
	allowed, err := approve(ctx, args, summary)
	if err != nil {
		if result, ok := err.(interface{ ToolResult() string }); ok {
			return errors.New(result.ToolResult())
		}
		return err
	}
	if !allowed {
		return errors.New("denied by user")
	}
	return ctx.Err()
}
