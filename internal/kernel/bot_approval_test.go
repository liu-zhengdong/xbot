package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/agent"
	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/tools"
)

func botApprovalFixture(t *testing.T, names ...string) (*Kernel, string) {
	t.Helper()
	k, _, endpoint := routineApprovalFixture(t)
	if err := k.Bot.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if err := bot.SetPrimary(k.Bot.Root, k.Bot.Name()); err != nil {
		t.Fatal(err)
	}
	k.ensureTurn().turnSender = "u1"
	for _, name := range names {
		b, err := bot.Create(k.Bot.Root, name, "purpose", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := b.SetOwners(k.Bot.Config().Owners); err != nil {
			t.Fatal(err)
		}
	}
	return k, endpoint
}

func TestBotSourceDeleteStrictApproval(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(map[bool]string{false: "deny", true: "allow"}[allow], func(t *testing.T) {
			k, endpoint := botApprovalFixture(t, "target")
			asked := 0
			approve := k.trackApprove(func(ctx context.Context, name, summary string) (bool, error) {
				asked++
				if name != "sh" || agent.ToolCallID(ctx) != "source-call" || !strings.Contains(string(agent.ToolArgs(ctx)), `"_xbot_bot":{"action":"delete","name":"target"}`) || !strings.Contains(summary, "repeat the original CLI command") {
					t.Errorf("wrong original delete approval %s %s %s", name, summary, agent.ToolArgs(ctx))
				}
				return allow, nil
			})
			runRoutineSource(t, k, approve, `{"command":"\"$XBOT_BIN\" delete target --self --json","description":"delete agreed bot"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
				reply, err := cliops.CallBotDelete(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), "target")
				if reply.Removed != allow || (err != nil) == allow {
					t.Errorf("removed=%v allow=%v stage=%s err=%v", reply.Removed, allow, reply.Stage, err)
				}
				return "completed", nil
			})
			if asked != 1 {
				t.Fatalf("asked=%d", asked)
			}
			_, err := bot.Load(k.Bot.Root, "target")
			if allow && !errors.Is(err, bot.ErrNotFound) || !allow && err != nil {
				t.Fatalf("actual target allow=%v err=%v", allow, err)
			}
		})
	}
}

func TestBotSourceDeleteMissingAndCompleted(t *testing.T) {
	k, endpoint := botApprovalFixture(t, "target")
	check := func(id string) {
		t.Helper()
		reply, err := cliops.CallBotDelete(context.Background(), endpoint, k.Bot.Root, id, "target")
		if err == nil || reply.Removed || !strings.Contains(reply.Error, "context unavailable") {
			t.Fatalf("invalid context accepted %+v %v", reply, err)
		}
	}
	check("")
	var completed string
	runRoutineSource(t, k, nil, `{"command":"delete target --self","description":"delete target"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		completed = tools.CallID(ctx)
		reply, err := cliops.CallBotDelete(ctx, endpoint, k.Bot.Root, completed, "target")
		if err == nil || reply.Removed || !strings.Contains(reply.Error, "approver") {
			t.Errorf("nil original approver accepted %+v %v", reply, err)
		}
		return "completed", nil
	})
	check(completed)
	if _, err := bot.Load(k.Bot.Root, "target"); err != nil {
		t.Fatal(err)
	}
	k.Permissive = true
	reply, err := cliops.CallBotDelete(context.Background(), endpoint, k.Bot.Root, "", "target")
	if err != nil || !reply.Removed {
		t.Fatalf("permissive path unnecessarily needs origin %+v %v", reply, err)
	}
}

func TestBotSourceDeleteParkTargetCanonical(t *testing.T) {
	k, endpoint := botApprovalFixture(t, "targeta", "targetb")
	owner := k.ensureTurn()
	path := filepath.Join(t.TempDir(), "target")
	write := func(name string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("targeta")
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
		if name != "sh" || !strings.Contains(string(agent.ToolArgs(ctx)), "_xbot_bot") {
			t.Errorf("grant for wrong call %s %s", name, agent.ToolArgs(ctx))
		}
		if asked == 1 {
			close(started)
			return <-decision, nil
		}
		return false, nil
	}))
	attempt := func() cliops.BotDeleteReply {
		t.Helper()
		var reply cliops.BotDeleteReply
		runRoutineSource(t, k, approve, `{"command":"\"$XBOT_BIN\" delete \"$(cat target)\" --self --json","description":"delete agreed target"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
			data, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			reply, _ = cliops.CallBotDelete(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), string(data))
			return "completed", nil
		})
		return reply
	}
	var first cliops.BotDeleteReply
	done := make(chan struct{})
	go func() { defer close(done); first = attempt() }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("original approval not reached")
	}
	owner.inter.mu.Lock()
	owner.inter.parkAllLocked()
	owner.inter.mu.Unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("park did not return")
	}
	if first.Removed || !strings.Contains(first.Error, "not run yet") {
		t.Fatalf("park removed target %+v", first)
	}
	decision <- true
	select {
	case <-wake:
	case <-time.After(5 * time.Second):
		t.Fatal("late grant not delivered")
	}
	owner.inter.mu.Lock()
	owner.inter.rearmLocked()
	owner.inter.mu.Unlock()
	write("targetb")
	if reply := attempt(); reply.Removed || asked != 2 {
		t.Fatalf("B consumed A %+v asked=%d", reply, asked)
	}
	if reply := attempt(); reply.Removed || asked != 2 || !strings.Contains(reply.Error, "already declined") {
		t.Fatalf("canonical B denial not retained %+v asked=%d", reply, asked)
	}
	write("targeta")
	if reply := attempt(); !reply.Removed || asked != 2 {
		t.Fatalf("same A missed grant %+v asked=%d", reply, asked)
	}
	if _, err := bot.Load(k.Bot.Root, "targeta"); !errors.Is(err, bot.ErrNotFound) {
		t.Fatalf("A still present %v", err)
	}
	if _, err := bot.Load(k.Bot.Root, "targetb"); err != nil {
		t.Fatalf("B changed %v", err)
	}
}

func TestResearchBotDirectCommandsUseBoundSource(t *testing.T) {
	k, _, _ := freshBot(t)
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	defer k.CloseOperations()
	interpreter := &boundReadOnlyInterpreter{}
	shell := readOnlyShell{Tool: interpreter, k: k}
	for _, command := range []string{`"$XBOT_BIN" list --self --json`, `"$XBOT_BIN" info --self --steps 10 --json`, `"$XBOT_BIN" new --help`} {
		raw, _ := json.Marshal(map[string]string{"command": command})
		if _, err := shell.Run(context.Background(), raw); err != nil {
			t.Fatalf("read/help rejected %s %v", command, err)
		}
	}
	for _, command := range []string{`"$XBOT_BIN" list`, `"$XBOT_BIN" info`, `"$XBOT_BIN" new target --self --persona purpose`, `"$XBOT_BIN" delete target --self`, `"$XBOT_BIN" --root /other list --self`, `"$XBOT_BIN" info --self; echo injected`, `xbot info --self`} {
		raw, _ := json.Marshal(map[string]string{"command": command})
		if _, err := shell.Run(context.Background(), raw); err == nil {
			t.Fatalf("write/other source accepted %s", command)
		}
	}
	if interpreter.calls != 3 {
		t.Fatalf("rejected call reached interpreter %d", interpreter.calls)
	}
}

func TestRuntimeApprovalRoutineDenialWithoutNeed(t *testing.T) {
	k, tm, endpoint := routineApprovalFixture(t)
	on := true
	k.PermissionsOn = func() bool { return on }
	command := "printf marker > " + precheckQuote(filepath.Join(k.Bot.Root, "env"))
	asked := 0
	approve := k.trackApprove(func(context.Context, string, string) (bool, error) { asked++; return false, nil })
	attempt := func() cliops.RoutineWriteReply {
		var reply cliops.RoutineWriteReply
		runRoutineSource(t, k, approve, `{"command":"routine create --self --args-file same.json","description":"configure"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
			reply, _ = cliops.CallRoutineCreate(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.RoutineCreate{ID: "denied", Prompt: "p", Cron: "0 * * * *", Precheck: &command})
			return "completed", nil
		})
		return reply
	}
	if reply := attempt(); reply.Applied || asked != 1 {
		t.Fatalf("first denial %+v asked=%d", reply, asked)
	}
	on = false
	if reply := attempt(); reply.Applied || asked != 1 || !strings.Contains(reply.Error, "already declined") {
		t.Fatalf("no-need bypassed canonical denial %+v asked=%d", reply, asked)
	}
	if list, err := tm.List(); err != nil || len(list) != 0 {
		t.Fatalf("denial wrote %+v %v", list, err)
	}
}
