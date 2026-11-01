package kernel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/agent"
	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

func TestRoutineSourceParkGrantBindsEffectiveFile(t *testing.T) {
	k, tm, endpoint := routineApprovalFixture(t)
	owner := k.ensureTurn()
	owner.turnSender = "u1"
	path := filepath.Join(t.TempDir(), "patch.json")
	commandA := "printf A > " + precheckQuote(filepath.Join(k.Bot.Root, "env"))
	commandB := "printf B >> " + precheckQuote(filepath.Join(k.Bot.Root, "env"))
	write := func(command string) {
		t.Helper()
		raw, err := json.Marshal(cliops.RoutineCreate{Prompt: "p", Cron: "0 * * * *", Precheck: &command})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(commandA)
	raw, _ := json.Marshal(map[string]string{"command": "\"$XBOT_BIN\" routine create --self --args-file " + precheckQuote(path), "description": "configure routine"})
	started, decision := make(chan struct{}), make(chan bool)
	wake := make(chan struct{}, 1)
	SetWaker(k.Bot.Root, func(*bot.Bot) {
		select {
		case wake <- struct{}{}:
		default:
		}
	})
	defer SetWaker(k.Bot.Root, nil)
	asked := 0
	approve := k.parkable(owner, k.trackApprove(func(ctx context.Context, name, summary string) (bool, error) {
		asked++
		if name != "sh" || !strings.Contains(summary, "--args-file") || !strings.Contains(string(agent.ToolArgs(ctx)), "_xbot_routine") {
			t.Errorf("uncallable grant identity: %s %s", name, summary)
		}
		if asked == 1 {
			close(started)
			return <-decision, nil
		}
		return false, nil
	}))
	attempt := func(callID string) cliops.RoutineWriteReply {
		t.Helper()
		var reply cliops.RoutineWriteReply
		p := &scripted{resps: []provider.Message{toolCall(callID, "sh", string(raw))}}
		ctx := context.WithValue(context.Background(), turnKey{}, owner)
		shell := routineSourceShell{run: func(ctx context.Context, executed json.RawMessage) (string, error) {
			if string(executed) != string(raw) {
				t.Errorf("actual Shell.Run args changed: %s", executed)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			var in cliops.RoutineCreate
			if err := json.Unmarshal(data, &in); err != nil {
				return "", err
			}
			reply, _ = cliops.CallRoutineCreate(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), in)
			return "completed", nil
		}}
		if _, err := agent.Run(ctx, p, []tools.Tool{shell}, nil, agent.Options{OneRound: true, Approve: approve, RunTool: k.runToolCall}); err != nil {
			t.Fatal(err)
		}
		return reply
	}
	firstDone := make(chan struct{})
	var first cliops.RoutineWriteReply
	go func() { defer close(firstDone); first = attempt("parked-A") }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("approval not reached")
	}
	owner.inter.mu.Lock()
	owner.inter.parkAllLocked()
	owner.inter.mu.Unlock()
	select {
	case <-firstDone:
	case <-time.After(5 * time.Second):
		t.Fatal("park did not return original call")
	}
	if first.Applied || !strings.Contains(first.Error, "not run yet") {
		t.Fatalf("parked operation changed definition: %+v", first)
	}
	if list, err := tm.List(); err != nil || len(list) != 0 {
		t.Fatalf("parked save: %+v %v", list, err)
	}
	decision <- true
	select {
	case <-wake:
	case <-time.After(5 * time.Second):
		t.Fatal("late original grant not delivered")
	}
	owner.inter.mu.Lock()
	owner.inter.rearmLocked()
	owner.inter.mu.Unlock()
	write(commandB)
	if reply := attempt("changed-B"); reply.Applied || asked != 2 {
		t.Fatalf("B reused A grant: %+v asked=%d", reply, asked)
	}
	if reply := attempt("declined-B-retry"); reply.Applied || asked != 2 || !strings.Contains(reply.Error, "already declined") {
		t.Fatalf("same canonical denial not enforced: %+v asked=%d", reply, asked)
	}
	write(commandA)
	last := attempt("same-A-new-provider-id")
	if !last.Applied || asked != 2 || !strings.HasPrefix(last.ID, "r") {
		t.Fatalf("same canonical A failed to consume original grant: %+v asked=%d", last, asked)
	}
	list, err := tm.List()
	if err != nil || len(list) != 1 || list[0].Precheck != commandA || list[0].ID != last.ID {
		t.Fatalf("grant definitions: %+v %v", list, err)
	}
}

func TestRoutineSourceIdentityAndCompletedCall(t *testing.T) {
	k, tm, endpoint := routineApprovalFixture(t)
	var callID string
	command := "true"
	other := &Kernel{Bot: k.Bot, Links: []link.Link{tm}, WorkDir: k.WorkDir, shell: k.shellJobs()}
	if err := other.StartOperations(); err != nil {
		t.Fatal(err)
	}
	defer other.CloseOperations()
	otherEndpoint := ""
	for _, env := range other.CLIEnvironment(nil) {
		if value, ok := strings.CutPrefix(env, "XBOT_ENDPOINT="); ok {
			otherEndpoint = value
		}
	}
	runRoutineSource(t, k, nil, `{"command":"routine create --args-file file.json","description":"configure routine"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		callID = tools.CallID(ctx)
		in := cliops.RoutineCreate{ID: "foreign", Prompt: "p", Cron: "0 * * * *", Precheck: &command}
		reply, err := cliops.CallRoutineCreate(ctx, otherEndpoint, k.Bot.Root, callID, in)
		if err == nil || reply.Applied || !strings.Contains(reply.Error, "context unavailable") {
			t.Errorf("foreign source accepted: %+v %v", reply, err)
		}
		in.ID = "valid"
		reply, err = cliops.CallRoutineCreate(ctx, endpoint, k.Bot.Root, callID, in)
		if err != nil || !reply.Applied {
			t.Errorf("actual source rejected: %+v %v", reply, err)
		}
		return "completed", nil
	})
	in := cliops.RoutineCreate{ID: "completed", Prompt: "p", Cron: "0 * * * *", Precheck: &command}
	reply, err := cliops.CallRoutineCreate(context.Background(), endpoint, k.Bot.Root, callID, in)
	if err == nil || reply.Applied || !strings.Contains(reply.Error, "context unavailable") {
		t.Fatalf("completed context reused: %+v %v", reply, err)
	}
	list, err := tm.List()
	if err != nil || len(list) != 1 || list[0].ID != "valid" {
		t.Fatalf("source definitions: %+v %v", list, err)
	}
}

func TestRoutineSourceUpdateReviewsKeptCommand(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(map[bool]string{false: "deny", true: "allow"}[allow], func(t *testing.T) {
			k, tm, endpoint := routineApprovalFixture(t)
			command := "printf marker > " + precheckQuote(filepath.Join(k.Bot.Root, "env"))
			if err := tm.Save(link.Routine{ID: "kept", Prompt: "p", Cron: "0 * * * *", Enabled: false, Precheck: command}); err != nil {
				t.Fatal(err)
			}
			asked := 0
			runRoutineSource(t, k, k.trackApprove(func(_ context.Context, name, summary string) (bool, error) {
				asked++
				if name != "sh" || !strings.Contains(summary, command) {
					t.Errorf("kept command not reviewed: %s %s", name, summary)
				}
				return allow, nil
			}), `{"command":"routine update kept --self --args-file patch.json","description":"enable routine"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
				enabled := true
				reply, err := cliops.CallRoutineUpdate(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), "kept", cliops.RoutinePatch{Enabled: &enabled})
				if reply.Applied != allow || (err != nil) == allow {
					t.Errorf("kept update: %+v %v", reply, err)
				}
				return "completed", nil
			})
			r, err := tm.Load("kept")
			if err != nil || r.Enabled != allow || r.Precheck != command || asked != 1 {
				t.Fatalf("kept=%+v asked=%d err=%v", r, asked, err)
			}
		})
	}
}

func TestRoutineSourceCardUsesOriginalOwnerBranch(t *testing.T) {
	k, _, endpoint := routineApprovalFixture(t)
	owner := k.ensureTurn()
	command := "printf A > " + precheckQuote(filepath.Join(k.Bot.Root, "env"))
	asked := 0
	approve := func(ctx context.Context, name, summary string) (bool, error) {
		asked++
		if name != "sh" || BranchOf(ctx) != "bg-original" || agent.ToolCallID(ctx) != "source-call" || !strings.Contains(summary, "repeat the original CLI command") {
			t.Errorf("wrong original card context name=%s branch=%s id=%s summary=%s", name, BranchOf(ctx), agent.ToolCallID(ctx), summary)
		}
		return true, nil
	}
	runRoutineSource(t, k, approve, `{"command":"routine create --args-file file.json","description":"configure routine"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		// Prepare already froze the pre-fork branch. The actual owner changes
		// before its existing wrapped approver handles the nested operation.
		owner.mu.Lock()
		owner.branch = "bg-original"
		owner.mu.Unlock()
		reply, err := cliops.CallRoutineCreate(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.RoutineCreate{ID: "branch", Prompt: "p", Cron: "0 * * * *", Precheck: &command})
		if err != nil || !reply.Applied {
			t.Errorf("original owner: %+v %v", reply, err)
		}
		return "completed", nil
	})
	if asked != 1 {
		t.Fatalf("asked=%d", asked)
	}
}
