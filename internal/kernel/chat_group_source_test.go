package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"xbot/internal/agent"
	"xbot/internal/cliops"
	"xbot/internal/exec"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

// Use the existing HTTP source-ID decoder and real Jobs preparation. Only the
// routine callback is replaced so both resolver contexts can be inspected.
func chatGroupSourceListener(t *testing.T, k *Kernel, inspect func(context.Context) error) string {
	t.Helper()
	k.CloseOperations()
	server, err := cliops.Start(k.Bot.Root, cliops.Handlers{Routines: &cliops.RoutineHandlers{
		Create: func(ctx context.Context, _ cliops.RoutineCreate) (cliops.RoutineWriteReply, error) {
			err := inspect(ctx)
			return cliops.RoutineWriteReply{Applied: err == nil}, err
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	k.operations.mu.Lock()
	k.operations.server = server
	k.operations.mu.Unlock()
	t.Cleanup(k.CloseOperations)
	return server.Endpoint
}

func TestChatGroupRuntimeSourcePreparedFrozen(t *testing.T) {
	k, _, _ := routineApprovalFixture(t)
	owner := k.ensureTurn()
	owner.turnChain, owner.turnHops, owner.turnSender = "ch_prepared", 2, "u1"
	var originalPrepared *turn
	calls := 0
	endpoint := chatGroupSourceListener(t, k, func(ctx context.Context) error {
		calls++
		resolved, cleanup, err := k.runtimeSourceCall(ctx)
		if err != nil {
			return err
		}
		defer cleanup()
		frozen, _ := resolved.Value(turnKey{}).(*turn)
		if frozen == nil || frozen != originalPrepared || frozen == owner || frozen == k.curTurn() || frozen.turnChain != "ch_prepared" || frozen.turnHops != 2 || BranchOf(resolved) != "" {
			t.Errorf("resolver replaced prepared turn: frozen=%p prepared=%p owner=%p current=%p", frozen, originalPrepared, owner, k.curTurn())
		}
		original, ok := k.liveShell().CallContext(cliops.SourceCallID(ctx))
		if !ok || original.Value(turnKey{}) != resolved.Value(turnKey{}) || agent.ToolCallID(resolved) != "source-call" || tools.CallID(resolved) != cliops.SourceCallID(ctx) {
			t.Error("resolver lost real prepared Job or provider identity")
		}
		name, approve := agent.ToolApprover(resolved)
		if name != "sh" || approve != nil {
			t.Errorf("Normal source must accept original Shell with nil approver: %q", name)
		}
		if trusted, person := k.reviewIdentity(resolved); !trusted || person != "u1" {
			t.Errorf("frozen policy changed: trusted=%v person=%q", trusted, person)
		}
		card, release, err := k.runtimeApprovalSource(ctx)
		if err != nil {
			return err
		}
		defer release()
		if card.Value(turnKey{}) != owner || BranchOf(card) != "bg-original" || agent.ToolCallID(card) != "source-call" {
			t.Error("approval wrapper lost original owner's dynamic branch")
		}
		if trusted, person := k.reviewIdentity(card); !trusted || person != "u1" {
			t.Error("approval wrapper changed frozen policy")
		}
		return nil
	})
	runRoutineSource(t, k, nil, `{"command":"chat-group post --self --text hello","description":"post group message"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		originalPrepared, _ = ctx.Value(turnKey{}).(*turn)
		owner.mu.Lock()
		owner.branch, owner.turnChain, owner.turnHops = "bg-original", "ch_owner_changed", 3
		owner.turnTrusted, owner.turnSender = false, "changed"
		owner.mu.Unlock()
		k.turnMu.Lock()
		k.turn = &turn{turnChain: "ch_latest", turnHops: 0, branch: "latest"}
		k.turnMu.Unlock()
		reply, err := cliops.CallRoutineCreate(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.RoutineCreate{})
		if err != nil || !reply.Applied {
			t.Errorf("real nil-approver source rejected: %+v %v", reply, err)
		}
		return "completed", nil
	})
	if calls != 1 {
		t.Fatalf("source inspections=%d", calls)
	}
}

func TestChatGroupRuntimeSourceMissingFinishedForeign(t *testing.T) {
	k, _, _ := routineApprovalFixture(t)
	inspect := func(target *Kernel) func(context.Context) error {
		return func(ctx context.Context) error {
			resolved, cleanup, err := target.runtimeSourceCall(ctx)
			if cleanup != nil {
				cleanup()
			}
			if err != nil && (resolved != nil || cleanup != nil) {
				t.Error("failed resolution returned a context or cleanup")
			}
			return err
		}
	}
	endpoint := chatGroupSourceListener(t, k, inspect(k))
	other := &Kernel{Bot: k.Bot, WorkDir: k.WorkDir, shell: k.shellJobs()}
	foreign := chatGroupSourceListener(t, other, inspect(other))
	check := func(endpoint, id string) {
		t.Helper()
		reply, err := cliops.CallRoutineCreate(context.Background(), endpoint, k.Bot.Root, id, cliops.RoutineCreate{})
		if err == nil || reply.Applied || reply.Error != "source approval context unavailable" {
			t.Errorf("unavailable source accepted id=%q: %+v %v", id, reply, err)
		}
	}
	check(endpoint, "")
	check(endpoint, "not-a-real-call")
	var completed string
	runRoutineSource(t, k, nil, `{"command":"chat-group post --self --text hello"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		completed = tools.CallID(ctx)
		check(foreign, completed)
		reply, err := cliops.CallRoutineCreate(ctx, endpoint, k.Bot.Root, completed, cliops.RoutineCreate{})
		if err != nil || !reply.Applied {
			t.Errorf("same listener real source rejected: %+v %v", reply, err)
		}
		return "completed", nil
	})
	check(endpoint, completed)
	if _, ok := k.liveShell().CallContext(completed); ok {
		t.Fatal("completed source remained live")
	}
}

// A real non-Shell Job carries provider metadata but never a Shell source marker.
type chatGroupNonShell struct{ routineSourceShell }

func (chatGroupNonShell) Name() string { return "fixture_non_shell" }

func TestChatGroupRuntimeSourceNonShellAndNoOwner(t *testing.T) {
	for _, noOwner := range []bool{false, true} {
		name := "non_shell"
		if noOwner {
			name = "shell_without_original_owner_or_child"
		}
		t.Run(name, func(t *testing.T) {
			k, _, _ := routineApprovalFixture(t)
			endpoint := chatGroupSourceListener(t, k, func(ctx context.Context) error {
				resolved, cleanup, err := k.runtimeSourceCall(ctx)
				if cleanup != nil {
					cleanup()
				}
				if resolved != nil || err == nil {
					t.Error("source without Shell identity or original owner was accepted")
				}
				return err
			})
			shell := routineSourceShell{run: func(ctx context.Context, _ json.RawMessage) (string, error) {
				if _, ok := k.liveShell().CallContext(tools.CallID(ctx)); !ok {
					t.Error("fixture did not reach a real running Job")
				}
				reply, err := cliops.CallRoutineCreate(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.RoutineCreate{})
				if err == nil || reply.Applied || reply.Error != "source approval context unavailable" {
					t.Errorf("invalid actual source accepted: %+v %v", reply, err)
				}
				return "completed", nil
			}}
			var tool tools.Tool = chatGroupNonShell{shell}
			ctx := context.WithValue(context.Background(), turnKey{}, k.ensureTurn())
			if noOwner {
				tool, ctx = shell, context.Background()
			}
			p := &scripted{resps: []provider.Message{toolCall("invalid-source", tool.Name(), `{}`)}}
			if _, err := agent.Run(ctx, p, []tools.Tool{tool}, nil, agent.Options{OneRound: true, RunTool: k.runToolCall}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestChatGroupRuntimeSourceCancellation(t *testing.T) {
	k, _, _ := routineApprovalFixture(t)
	cancelOriginal := false
	endpoint := chatGroupSourceListener(t, k, func(ctx context.Context) error {
		if cancelOriginal {
			resolved, cleanup, err := k.runtimeSourceCall(ctx)
			if resolved != nil || cleanup != nil || !errors.Is(err, context.Canceled) {
				t.Errorf("cancelled original accepted: ctx=%v cleanup=%v err=%v", resolved, cleanup != nil, err)
			}
			return err
		}
		request, cancel := context.WithCancel(ctx)
		defer cancel()
		resolved, cleanup, err := k.runtimeSourceCall(request)
		if err != nil {
			return err
		}
		defer cleanup()
		cancel()
		select {
		case <-resolved.Done():
			if !errors.Is(resolved.Err(), context.Canceled) {
				t.Errorf("wrong request cancellation: %v", resolved.Err())
			}
		case <-time.After(time.Second):
			t.Error("HTTP parent cancellation did not reach resolved context")
		}
		original, ok := k.liveShell().CallContext(cliops.SourceCallID(ctx))
		if !ok || original.Err() != nil {
			t.Error("request cleanup cancelled the original Shell")
		}
		return nil
	})
	runRoutineSource(t, k, nil, `{"command":"chat-group post --self --text hello"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		id := tools.CallID(ctx)
		reply, err := cliops.CallRoutineCreate(ctx, endpoint, k.Bot.Root, id, cliops.RoutineCreate{})
		if err != nil || !reply.Applied {
			t.Errorf("request cancellation fixture failed: %+v %v", reply, err)
		}
		cancelOriginal = true
		if _, err := k.liveShell().Cancel(id); err != nil {
			return "", err
		}
		// Keep this HTTP request alive while the actual source Job is cancelled.
		reply, err = cliops.CallRoutineCreate(context.Background(), endpoint, k.Bot.Root, id, cliops.RoutineCreate{})
		if err == nil || reply.Applied || !strings.Contains(reply.Error, context.Canceled.Error()) {
			t.Errorf("cancelled source response %+v %v", reply, err)
		}
		return "completed", nil
	})
}

func TestChatGroupRuntimeSourceActualChildRebind(t *testing.T) {
	p := &scripted{resps: []provider.Message{
		toolCall("child-source", "sh", `{"command":"chat-group post --self --text child","description":"post child message"}`),
		{Role: provider.RoleAssistant, Content: provider.TextContent("child complete")},
	}}
	k, b, box := dispatchKernel(t, p)
	k.ensureTurn().turnTrusted, k.ensureTurn().turnSender = true, "u1"
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := k.Close(); err != nil {
			t.Error(err)
		}
	})
	endpoint := chatGroupSourceListener(t, k, func(ctx context.Context) error {
		resolved, cleanup, err := k.runtimeSourceCall(ctx)
		if err != nil {
			return err
		}
		defer cleanup()
		source, _ := resolved.Value(routineCallSourceKey{}).(routineCallSource)
		original, ok := k.liveShell().CallContext(cliops.SourceCallID(ctx))
		if !ok || !source.child || source.owner != nil || source.trusted || source.person != "" || resolved.Value(turnKey{}) == nil || resolved.Value(turnKey{}) != original.Value(turnKey{}) || agent.ToolCallID(resolved) != "child-source" {
			t.Error("actual child lost prepared context or gained owner policy")
		}
		card, release, err := k.runtimeApprovalSource(ctx)
		if err != nil {
			return err
		}
		defer release()
		if owner, _ := card.Value(turnKey{}).(*turn); owner != nil {
			t.Error("child approval wrapper borrowed a main owner")
		}
		if trusted, person := k.reviewIdentity(card); trusted || person != "" {
			t.Error("child approval wrapper changed explicit child identity")
		}
		return nil
	})
	_ = k.ensureExec()
	local, ok := k.execSnapshot().(*exec.Local)
	if !ok {
		t.Fatalf("executor %T is not Local", k.execSnapshot())
	}
	called := make(chan error, 1)
	originalTools := local.ToolsFor
	local.ToolsFor = func(req exec.StartRequest) []tools.Tool {
		list := originalTools(req)
		for i, tool := range list {
			if tool.Name() == "sh" {
				list[i] = routineSourceShell{run: func(ctx context.Context, _ json.RawMessage) (string, error) {
					reply, err := cliops.CallRoutineCreate(ctx, endpoint, b.Root, tools.CallID(ctx), cliops.RoutineCreate{})
					if err == nil && !reply.Applied {
						err = errors.New("child source not applied")
					}
					called <- err
					return "completed", err
				}}
			}
		}
		return list
	}
	task, err := k.StartSubagent(context.Background(), SubagentRequest{Type: "dev", Task: "post child group message"})
	if err != nil {
		t.Fatal(err)
	}
	if done := waitEvent(t, box, exec.EvDone); done.Sender != task.ID {
		t.Fatalf("completion belongs to %q, want %q", done.Sender, task.ID)
	}
	select {
	case err := <-called:
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("actual child never called the resolver over HTTP")
	}
}
