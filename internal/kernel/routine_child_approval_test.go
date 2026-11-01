package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"xbot/internal/agent"
	"xbot/internal/cliops"
	"xbot/internal/exec"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

func TestRoutineLocalChildUsesOriginalApprover(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(fmt.Sprintf("allow_%v", allow), func(t *testing.T) {
			p := &scripted{resps: []provider.Message{
				toolCall("child-routine-source", "sh", `{"command":"\"$XBOT_BIN\" routine create --self --args-file child.json","description":"configure child routine"}`),
				{Role: provider.RoleAssistant, Content: provider.TextContent("child complete")},
			}}
			k, b, box := dispatchKernel(t, p)
			tm := &link.Timer{Dir: b.RoutineDir()}
			k.Links = append(k.Links, tm)
			k.ensureTurn().turnTrusted = true
			k.ensureTurn().turnSender = "u1"
			marker := filepath.Join(b.Root, "bots", b.Name(), "memory", "child-marker")
			if err := os.MkdirAll(filepath.Dir(marker), 0700); err != nil {
				t.Fatal(err)
			}
			command := "printf marker > " + precheckQuote(marker)
			// The same command needs no approval under the current trusted main
			// turn. Borrowing that turn would silently bypass the child's card.
			if need, summary, err := k.reviewPrecheck(context.Background(), command, "main reference"); err != nil || need {
				t.Fatalf("trusted main reference: need=%v summary=%q err=%v", need, summary, err)
			}
			k.Approve = func(context.Context, string, string) (bool, error) {
				t.Error("child borrowed the main approver")
				return false, nil
			}
			var asked atomic.Int32
			k.BackgroundApprove = func(ctx context.Context, name, summary string) (bool, error) {
				asked.Add(1)
				if name != "sh" || agent.ToolCallID(ctx) != "child-routine-source" || !strings.Contains(summary, "untrusted turn: memory access") || !strings.Contains(string(agent.ToolArgs(ctx)), "_xbot_routine") {
					t.Errorf("wrong child approval: name=%q call=%q summary=%q args=%s", name, agent.ToolCallID(ctx), summary, agent.ToolArgs(ctx))
				}
				if owner, _ := ctx.Value(turnKey{}).(*turn); owner != nil {
					t.Error("child approval borrowed a main turn owner")
				}
				return allow, nil
			}
			if err := k.StartOperations(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(k.CloseOperations)
			t.Cleanup(func() {
				if err := k.Close(); err != nil {
					t.Error(err)
				}
			})
			endpoint := operationEndpoint(k)
			_ = k.ensureExec()
			local, ok := k.execSnapshot().(*exec.Local)
			if !ok {
				t.Fatalf("executor %T is not Local", k.execSnapshot())
			}
			type callResult struct {
				reply cliops.RoutineWriteReply
				err   error
				jobID string
			}
			called := make(chan callResult, 1)
			originalTools := local.ToolsFor
			// Replace process execution only. StartSubagent, Local.RunLoop,
			// Local.RunTool, Jobs and its original subApprover remain real.
			local.ToolsFor = func(req exec.StartRequest) []tools.Tool {
				list := originalTools(req)
				for i, tool := range list {
					if tool.Name() == "sh" {
						list[i] = routineSourceShell{run: func(ctx context.Context, _ json.RawMessage) (string, error) {
							id := tools.CallID(ctx)
							reply, err := cliops.CallRoutineCreate(ctx, endpoint, b.Root, id, cliops.RoutineCreate{ID: "child-check", Prompt: "inspect", Cron: "0 * * * *", Precheck: &command})
							called <- callResult{reply, err, id}
							raw, _ := json.Marshal(reply)
							return string(raw), err
						}}
					}
				}
				return list
			}
			task, err := k.StartSubagent(context.Background(), SubagentRequest{Type: "dev", Task: "configure this routine"})
			if err != nil {
				t.Fatal(err)
			}
			done := waitEvent(t, box, exec.EvDone)
			if done.Sender != task.ID {
				t.Fatalf("completion belongs to %q, want child %q", done.Sender, task.ID)
			}
			if asked.Load() != 1 {
				t.Fatalf("background approvals=%d, want 1", asked.Load())
			}
			select {
			case result := <-called:
				if result.jobID == "" || result.reply.Applied != allow || (result.err != nil) == allow {
					t.Fatalf("source call job=%q reply=%+v err=%v", result.jobID, result.reply, result.err)
				}
				if !allow && !strings.Contains(result.reply.Error, "denied by user") {
					t.Fatalf("denied source reply %+v", result.reply)
				}
			default:
				t.Fatal("child never called source HTTP from a real Job")
			}
			saved, err := tm.Load("child-check")
			if allow {
				if err != nil || saved.Precheck != command || !saved.Enabled {
					t.Fatalf("approved child definition %+v: %v", saved, err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("denied child definition persisted %+v: %v", saved, err)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("CRUD executed the child precheck: %v", err)
			}
		})
	}
}
