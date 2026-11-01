package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"xbot/internal/provider"
	"xbot/internal/tools"
)

type callApprovalTool struct {
	confirm bool
	run     func(context.Context, json.RawMessage) (string, error)
}

func (*callApprovalTool) Name() string        { return "sh" }
func (*callApprovalTool) Description() string { return "Exercise the original call approval" }
func (*callApprovalTool) Parameters() any     { return map[string]any{"type": "object"} }
func (s *callApprovalTool) Confirm(json.RawMessage) (bool, string, error) {
	return s.confirm, "original shell command", nil
}
func (s *callApprovalTool) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	return s.run(ctx, raw)
}

func TestCallApproverPreservesOriginalCall(t *testing.T) {
	type markerKey struct{}
	const original = `{ "command": "xbot routine create --self", "description": "Create routine" }`
	const approvalOnly = `{"command":"xbot routine create --self","routine":{"action":"create","effectivePrecheck":"test -f ready"}}`
	for _, available := range []bool{true, false} {
		t.Run(fmt.Sprintf("approver=%v", available), func(t *testing.T) {
			ctx := context.WithValue(context.Background(), markerKey{}, "original owner")
			jobs := tools.NewJobs(t.TempDir())
			defer jobs.Shutdown()
			approvals, wrapped, runs := 0, 0, 0
			var approve Approver
			if available {
				originalApprove := func(ctx context.Context, name, summary string) (bool, error) {
					approvals++
					if name != "sh" || ToolCallID(ctx) != "provider-original-call" || ctx.Value(markerKey{}) != "original owner" {
						t.Errorf("approval identity: name=%q id=%q marker=%v", name, ToolCallID(ctx), ctx.Value(markerKey{}))
					}
					wantArgs, wantSummary := original, "original shell command"
					if approvals == 2 {
						wantArgs, wantSummary = approvalOnly, "approve effective precheck"
					}
					if string(ToolArgs(ctx)) != wantArgs || summary != wantSummary {
						t.Errorf("approval %d: args=%s summary=%q", approvals, ToolArgs(ctx), summary)
					}
					return true, nil
				}
				approve = func(ctx context.Context, name, summary string) (bool, error) {
					wrapped++
					return originalApprove(ctx, name, summary)
				}
			}
			tool := &callApprovalTool{confirm: available}
			tool.run = func(ctx context.Context, raw json.RawMessage) (string, error) {
				runs++
				name, call := ToolApprover(ctx)
				if name != tool.Name() || (call != nil) != available {
					return "", fmt.Errorf("ToolApprover = %q, available=%v", name, call != nil)
				}
				if ctx.Value(markerKey{}) != "original owner" || tools.CallID(ctx) == "" {
					return "", fmt.Errorf("lost prepared call identity")
				}
				if call != nil {
					ok, err := call(ctx, json.RawMessage(approvalOnly), "approve effective precheck")
					if err != nil || !ok {
						return "", fmt.Errorf("nested approval: approved=%v err=%v", ok, err)
					}
				}
				if string(raw) != original || string(ToolArgs(ctx)) != original || ToolCallID(ctx) != "provider-original-call" {
					return "", fmt.Errorf("execution arguments changed: raw=%s context=%s id=%s", raw, ToolArgs(ctx), ToolCallID(ctx))
				}
				return "ran original arguments", nil
			}
			p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
				ID: "provider-original-call", Type: "function", Function: provider.FunctionCall{Name: tool.Name(), Arguments: original},
			}})}}}
			got, err := Run(ctx, p, []tools.Tool{tool}, nil, Options{
				OneRound: true,
				Approve:  approve,
				RunTool: func(ctx context.Context, tool tools.Tool, raw json.RawMessage, invoke func(context.Context) tools.CallResult) tools.CallResult {
					return jobs.RunCall(ctx, tools.CallRequest{Name: tool.Name(), Run: invoke})
				},
			})
			if err != nil || len(got) != 2 || got[1].Text() != "ran original arguments" || runs != 1 {
				t.Fatalf("run: messages=%+v runs=%d err=%v", got, runs, err)
			}
			wantApprovals := 0
			if available {
				wantApprovals = 2
			}
			if approvals != wantApprovals || wrapped != wantApprovals {
				t.Fatalf("original approvals=%d wrapped approvals=%d want=%d", approvals, wrapped, wantApprovals)
			}
		})
	}
}
