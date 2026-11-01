package kernel

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"xbot/internal/agent"
	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/exec"
	"xbot/internal/mcp"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

func TestPersonaRuntimeStrictOriginalApproval(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		allow      bool
		valid      bool
	}{
		{"allow", " \n完整职责 A\u3000", true, true},
		{"deny", "完整职责 A", false, true},
		{"invalid_body_digest_only", strings.Repeat("x", cliops.BotMaxRequest-128), true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k, _, endpoint := routineApprovalFixture(t)
			old, err := k.Bot.ReadPersona()
			if err != nil {
				t.Fatal(err)
			}
			const raw = `{"command":"\"$XBOT_BIN\" persona set --self --file duties.txt --json","description":"save agreed duties","timeout_seconds":9}`
			original := map[string]json.RawMessage{}
			if err := json.Unmarshal([]byte(raw), &original); err != nil {
				t.Fatal(err)
			}
			asked := 0
			approve := k.trackApprove(func(ctx context.Context, name, summary string) (bool, error) {
				asked++
				if name != "sh" || agent.ToolCallID(ctx) != "source-call" || !strings.Contains(summary, "repeat the original CLI command") {
					t.Errorf("wrong original callback: %s %s %s", name, agent.ToolCallID(ctx), summary)
				}
				var args map[string]json.RawMessage
				if err := json.Unmarshal(agent.ToolArgs(ctx), &args); err != nil {
					t.Error(err)
					return false, err
				}
				var intent struct {
					Action     string `json:"action"`
					TextSHA256 string `json:"text_sha256"`
					Bytes      int    `json:"bytes"`
				}
				if err := json.Unmarshal(args["_xbot_persona"], &intent); err != nil {
					t.Error(err)
				}
				canonical := strings.TrimSpace(tc.text)
				if intent.Action != "set" || intent.TextSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(canonical))) || len(intent.TextSHA256) != 64 || intent.Bytes != len(canonical) || len(args["_xbot_persona"]) > 160 {
					t.Errorf("wrong complete canonical digest: %+v", intent)
				}
				delete(args, "_xbot_persona")
				if !reflect.DeepEqual(args, original) || len(agent.ToolArgs(ctx)) > len(raw)+256 {
					t.Error("approval copied the body or changed original Shell arguments")
				}
				return tc.allow, nil
			})
			attempt := func() {
				runRoutineSource(t, k, approve, raw, func(ctx context.Context, executed json.RawMessage) (string, error) {
					if string(executed) != raw || string(agent.ToolArgs(ctx)) != raw {
						t.Error("actual Shell.Run arguments changed")
					}
					if _, ok := k.liveShell().CallContext(tools.CallID(ctx)); !ok {
						t.Error("source is not an actual live Jobs call")
					}
					reply, err := cliops.CallBotPersona(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.BotPersona{Text: tc.text})
					written := tc.allow && tc.valid
					if reply.Written != written || (err == nil) != written {
						t.Errorf("original approval result %+v %v", reply, err)
					}
					return "completed", nil
				})
			}
			attempt()
			if !tc.allow {
				attempt()
			}
			if asked != 1 {
				t.Fatalf("SelfEdit approval/deniedBefore asked=%d", asked)
			}
			want := old
			if tc.allow && tc.valid {
				want = strings.TrimSpace(tc.text)
			}
			if got, err := k.Bot.ReadPersona(); err != nil || got != want {
				t.Fatalf("actual persona %q want %q: %v", got, want, err)
			}
		})
	}
}

func TestPersonaRuntimeStrictMissingFinishedForeignNoApprover(t *testing.T) {
	k, _, endpoint := routineApprovalFixture(t)
	old, err := k.Bot.ReadPersona()
	if err != nil {
		t.Fatal(err)
	}
	check := func(endpoint, id, reason string) {
		t.Helper()
		reply, err := cliops.CallBotPersona(context.Background(), endpoint, k.Bot.Root, id, cliops.BotPersona{Text: "must not be written"})
		if err == nil || reply.Written || reply.Effective != "" || !strings.Contains(reply.Error, reason) {
			t.Errorf("invalid source accepted: %+v %v", reply, err)
		}
	}
	check(endpoint, "", "context unavailable")
	other := &Kernel{Bot: k.Bot, WorkDir: k.WorkDir, shell: k.shellJobs()}
	if err := other.StartOperations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(other.CloseOperations)
	var completed string
	runRoutineSource(t, k, nil, `{"command":"persona set --self --file duties.txt","description":"save duties"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		completed = tools.CallID(ctx)
		check(operationEndpoint(other), completed, "context unavailable")
		check(endpoint, completed, "approver")
		return "completed", nil
	})
	check(endpoint, completed, "context unavailable")
	if got, err := k.Bot.ReadPersona(); err != nil || got != old {
		t.Fatalf("invalid source changed persona: %q %v", got, err)
	}
}

func TestPersonaRuntimeStrictFrozenBodyRename(t *testing.T) {
	k, _, endpoint := routineApprovalFixture(t)
	original := k.Bot
	manager := mcp.NewManager(context.Background(), mcp.Options{})
	t.Cleanup(func() { _ = manager.Close() })
	r, err := NewBotResources(context.Background(), original, manager)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	k.Resources = r
	path := filepath.Join(t.TempDir(), "duties.txt")
	const bodyA, bodyB = "approved frozen duties A", "changed file duties B"
	if err := os.WriteFile(path, []byte(" \n"+bodyA+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	asked := 0
	approve := func(ctx context.Context, name, summary string) (bool, error) {
		asked++
		if !r.route.TryLock() {
			t.Error("approval holds route lock")
			return false, nil
		}
		r.route.Unlock()
		if !k.operations.mu.TryLock() {
			t.Error("approval holds operations mutex")
			return false, nil
		}
		k.operations.mu.Unlock()
		if _, ok := r.Jobs.CallContext(tools.CallID(ctx)); !ok {
			t.Error("approval lost original running Jobs context")
		}
		if err := os.WriteFile(path, []byte(bodyB), 0600); err != nil {
			return false, err
		}
		if err := r.RenameTo("approved-current-source"); err != nil {
			return false, err
		}
		return true, nil
	}
	raw, _ := json.Marshal(map[string]string{"command": "\"$XBOT_BIN\" persona set --self --file " + precheckQuote(path), "description": "save duties"})
	runRoutineSource(t, k, approve, string(raw), func(ctx context.Context, executed json.RawMessage) (string, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		reply, err := cliops.CallBotPersona(ctx, endpoint, original.Root, tools.CallID(ctx), cliops.BotPersona{Text: string(data)})
		if err != nil || !reply.Written || reply.Name != "approved-current-source" || reply.Effective != "next_wake" || string(executed) != string(raw) {
			t.Errorf("approved current source %+v %v", reply, err)
		}
		return "completed", nil
	})
	if asked != 1 || k.Bot != original {
		t.Fatalf("original source/approval changed asked=%d", asked)
	}
	current, err := r.CurrentBot()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := current.ReadPersona(); err != nil || got != bodyA {
		t.Fatalf("did not write frozen A: %q %v", got, err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != bodyB {
		t.Fatalf("fixture file did not change to B: %q %v", got, err)
	}
	if _, err := os.Stat(bot.Dir(original.Root, original.Name())); !os.IsNotExist(err) {
		t.Fatalf("approved write revived old source path: %v", err)
	}
}

func TestPersonaRuntimeStrictCanonicalParkGrant(t *testing.T) {
	k, _, endpoint := routineApprovalFixture(t)
	owner := k.ensureTurn()
	owner.turnSender = "u1"
	old, err := k.Bot.ReadPersona()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "duties.txt")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("canonical A")
	started, decision := make(chan struct{}), make(chan bool, 1)
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
		if name != "sh" || !strings.Contains(string(agent.ToolArgs(ctx)), "_xbot_persona") {
			t.Error("park approval lost original Shell identity")
		}
		if asked == 1 {
			close(started)
			return <-decision, nil
		}
		return false, nil
	}))
	raw, _ := json.Marshal(map[string]string{"command": "\"$XBOT_BIN\" persona set --self --file " + precheckQuote(path), "description": "save duties"})
	attempt := func(providerID string) cliops.BotPersonaReply {
		var reply cliops.BotPersonaReply
		p := &scripted{resps: []provider.Message{toolCall(providerID, "sh", string(raw))}}
		ctx := context.WithValue(context.Background(), turnKey{}, owner)
		shell := routineSourceShell{run: func(ctx context.Context, executed json.RawMessage) (string, error) {
			if string(executed) != string(raw) {
				t.Error("canonical approval changed actual Shell.Run raw")
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			reply, _ = cliops.CallBotPersona(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.BotPersona{Text: string(data)})
			return "completed", nil
		}}
		if _, err := agent.Run(ctx, p, []tools.Tool{shell}, nil, agent.Options{OneRound: true, Approve: approve, RunTool: k.runToolCall}); err != nil {
			t.Error(err)
		}
		return reply
	}
	var first cliops.BotPersonaReply
	done := make(chan struct{})
	go func() { defer close(done); first = attempt("parked-A") }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("approval not reached")
	}
	owner.inter.mu.Lock()
	owner.inter.parkAllLocked()
	owner.inter.mu.Unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("parked source call did not return")
	}
	if first.Written || !strings.Contains(first.Error, "not run yet") {
		t.Fatalf("parked request wrote persona: %+v", first)
	}
	if got, err := k.Bot.ReadPersona(); err != nil || got != old {
		t.Fatalf("park changed old persona: %q %v", got, err)
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
	write("canonical B")
	if reply := attempt("changed-B"); reply.Written || asked != 2 {
		t.Fatalf("B consumed A grant %+v asked=%d", reply, asked)
	}
	if reply := attempt("denied-B"); reply.Written || asked != 2 || !strings.Contains(reply.Error, "already declined") {
		t.Fatalf("canonical denial bypassed %+v asked=%d", reply, asked)
	}
	write(" \tcanonical A\u3000\n")
	if reply := attempt("same-effective-A-new-provider-id"); !reply.Written || reply.Effective != "next_wake" || asked != 2 {
		t.Fatalf("same trimmed value missed original grant %+v asked=%d", reply, asked)
	}
	if got, err := k.Bot.ReadPersona(); err != nil || got != "canonical A" {
		t.Fatalf("grant did not write A: %q %v", got, err)
	}
}

func TestPersonaRuntimeStrictOriginalOwnerPolicyBranch(t *testing.T) {
	k, _, endpoint := routineApprovalFixture(t)
	owner := k.ensureTurn()
	owner.turnSender, owner.turnTrusted = "u1", true
	asked := 0
	approve := func(ctx context.Context, name, summary string) (bool, error) {
		asked++
		trusted, person := k.reviewIdentity(ctx)
		if name != "sh" || agent.ToolCallID(ctx) != "source-call" || BranchOf(ctx) != "bg-original" || !trusted || person != "u1" {
			t.Errorf("original frozen policy/dynamic owner lost: name=%s id=%s branch=%s trusted=%v person=%s", name, agent.ToolCallID(ctx), BranchOf(ctx), trusted, person)
		}
		return true, nil
	}
	runRoutineSource(t, k, approve, `{"command":"persona set --self --file duties.txt","description":"save duties"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		owner.mu.Lock()
		owner.branch, owner.turnSender, owner.turnTrusted = "bg-original", "foreign", false
		owner.mu.Unlock()
		reply, err := cliops.CallBotPersona(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.BotPersona{Text: "owner approved duties"})
		if err != nil || !reply.Written {
			t.Errorf("original owner rejected %+v %v", reply, err)
		}
		return "completed", nil
	})
	if asked != 1 {
		t.Fatalf("SelfEdit did not call original approver once: %d", asked)
	}
}

func TestPersonaRuntimeStrictActualChildApprover(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(fmt.Sprintf("allow_%v", allow), func(t *testing.T) {
			p := &scripted{resps: []provider.Message{
				toolCall("child-persona-source", "sh", `{"command":"\"$XBOT_BIN\" persona set --self --file child.txt","description":"save child duties"}`),
				{Role: provider.RoleAssistant, Content: provider.TextContent("child complete")},
			}}
			k, b, box := dispatchKernel(t, p)
			old, err := b.ReadPersona()
			if err != nil {
				t.Fatal(err)
			}
			k.ensureTurn().turnTrusted = true
			k.ensureTurn().turnSender = "u1"
			k.Approve = func(context.Context, string, string) (bool, error) {
				t.Error("child borrowed main approver")
				return false, nil
			}
			var asked atomic.Int32
			k.BackgroundApprove = func(ctx context.Context, name, summary string) (bool, error) {
				asked.Add(1)
				trusted, person := k.reviewIdentity(ctx)
				owner, _ := ctx.Value(turnKey{}).(*turn)
				if name != "sh" || agent.ToolCallID(ctx) != "child-persona-source" || trusted || person != "" || owner != nil || !strings.Contains(string(agent.ToolArgs(ctx)), "_xbot_persona") {
					t.Errorf("actual child policy/callback lost: name=%s id=%s trusted=%v person=%s owner=%p", name, agent.ToolCallID(ctx), trusted, person, owner)
				}
				return allow, nil
			}
			if err := k.StartOperations(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(k.CloseOperations)
			t.Cleanup(func() { _ = k.Close() })
			endpoint := operationEndpoint(k)
			_ = k.ensureExec()
			local, ok := k.execSnapshot().(*exec.Local)
			if !ok {
				t.Fatalf("executor %T is not actual Local", k.execSnapshot())
			}
			type result struct {
				reply cliops.BotPersonaReply
				err   error
				jobID string
			}
			called := make(chan result, 1)
			originalTools := local.ToolsFor
			local.ToolsFor = func(req exec.StartRequest) []tools.Tool {
				list := originalTools(req)
				for i, tool := range list {
					if tool.Name() == "sh" {
						// Keep Local.RunTool, the actual child policy and its original
						// subApprover. Replace process execution only.
						list[i] = routineSourceShell{run: func(ctx context.Context, _ json.RawMessage) (string, error) {
							id := tools.CallID(ctx)
							reply, err := cliops.CallBotPersona(ctx, endpoint, b.Root, id, cliops.BotPersona{Text: "actual child approved duties"})
							called <- result{reply, err, id}
							raw, _ := json.Marshal(reply)
							return string(raw), err
						}}
					}
				}
				return list
			}
			task, err := k.StartSubagent(context.Background(), SubagentRequest{Type: "dev", Task: "save agreed duties"})
			if err != nil {
				t.Fatal(err)
			}
			if done := waitEvent(t, box, exec.EvDone); done.Sender != task.ID {
				t.Fatalf("wrong actual child completion: %s", done.Sender)
			}
			if asked.Load() != 1 {
				t.Fatalf("original child callback count=%d", asked.Load())
			}
			select {
			case got := <-called:
				if got.jobID == "" || got.reply.Written != allow || (got.err == nil) != allow {
					t.Fatalf("actual child source %+v", got)
				}
			default:
				t.Fatal("actual child never reached source HTTP")
			}
			want := old
			if allow {
				want = "actual child approved duties"
			}
			if got, err := b.ReadPersona(); err != nil || got != want {
				t.Fatalf("actual child writer %q want %q: %v", got, want, err)
			}
		})
	}
}
