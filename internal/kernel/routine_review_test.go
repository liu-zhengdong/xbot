package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/agent"
	"xbot/internal/cliops"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/machine"
	"xbot/internal/provider"
	"xbot/internal/review"
	"xbot/internal/tools"
)

func TestRoutinePrecheckUsesShellReview(t *testing.T) {
	b := testBot(t, inbox.NameTimer)
	mode, on := machine.Allow, true
	k := &Kernel{Bot: b, Machine: func() (string, error) { return mode, nil }, PermissionsOn: func() bool { return on }}
	k.ensureTurn().turnTrusted = true
	protected := "printf marker > " + precheckQuote(filepath.Join(b.Root, "env"))
	for _, tc := range []struct {
		name, command, mode string
		on, need, reject    bool
	}{
		{"protected_on", protected, machine.Allow, true, true, false},
		{"protected_off", protected, machine.Allow, false, false, false},
		{"ask", "true", machine.Ask, true, true, false},
		{"off", "true", machine.Off, true, false, true},
		{"delete", "rm -f disposable", machine.Allow, true, true, false},
		{"send", "curl -X POST https://example.invalid", machine.Allow, true, true, false},
		{"delete_permissions_off", "rm -f disposable", machine.Allow, false, false, false},
		{"ordinary", "true", machine.Allow, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mode, on = tc.mode, tc.on
			args, _ := json.Marshal(map[string]string{"description": "test review", "command": tc.command})
			need, _, err := (reviewedTool{Tool: &tools.Shell{}, k: k}).Confirm(args)
			if need != tc.need || (err != nil) != tc.reject {
				t.Fatalf("reference shell need=%v err=%v", need, err)
			}
			got, summary, err := k.reviewPrecheck(context.Background(), tc.command, "create routine")
			if got != tc.need || (err != nil) != tc.reject {
				t.Fatalf("precheck need=%v err=%v", got, err)
			}
			if tc.need && !strings.Contains(summary, "precheck sh -c") {
				t.Fatalf("card lacks command: %s", summary)
			}
			if tc.need && strings.HasPrefix(tc.name, "protected") && !strings.Contains(summary, review.ProtectedSuffix) {
				t.Fatalf("missing protected summary: %s", summary)
			}
		})
	}
	if _, _, err := k.reviewPrecheck(context.Background(), strings.Repeat("x", tools.MaxPrecheckBytes+1), "create"); err == nil {
		t.Fatal("oversized precheck accepted")
	}
}

func TestRoutinePrecheckReviewsProcessCWD(t *testing.T) {
	b := testBot(t, inbox.NameTimer)
	t.Chdir(b.Root)
	k := &Kernel{Bot: b, WorkDir: t.TempDir()}
	need, summary, err := k.reviewPrecheck(context.Background(), "printf marker > env", "create routine")
	if err != nil || !need || !strings.Contains(summary, review.ProtectedSuffix) {
		t.Fatalf("need=%v summary=%q err=%v", need, summary, err)
	}
}

// This is an author unit at the real Agent -> Jobs -> source HTTP boundary.
// The Shell fixture replaces process execution only; it never injects a Job or
// approval identity into HTTP. The real RunCall supplies that identity.
type routineSourceShell struct {
	run func(context.Context, json.RawMessage) (string, error)
}

func (routineSourceShell) Name() string        { return "sh" }
func (routineSourceShell) Description() string { return "fixture shell" }
func (routineSourceShell) Parameters() any     { return map[string]any{"type": "object"} }
func (routineSourceShell) Confirm(json.RawMessage) (bool, string, error) {
	return false, "original CLI command", nil
}
func (s routineSourceShell) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	return s.run(ctx, raw)
}

func routineApprovalFixture(t *testing.T) (*Kernel, *link.Timer, string) {
	t.Helper()
	b := testBot(t, inbox.NameTimer)
	tm := &link.Timer{Dir: b.RoutineDir()}
	k := &Kernel{Bot: b, Links: []link.Link{tm}, WorkDir: t.TempDir()}
	k.ensureTurn().turnTrusted = true
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(k.CloseOperations)
	endpoint := ""
	for _, item := range k.CLIEnvironment(nil) {
		if value, ok := strings.CutPrefix(item, "XBOT_ENDPOINT="); ok {
			endpoint = value
		}
	}
	return k, tm, endpoint
}

func runRoutineSource(t *testing.T, k *Kernel, approve agent.Approver, raw string, run func(context.Context, json.RawMessage) (string, error)) {
	t.Helper()
	p := &scripted{resps: []provider.Message{toolCall("source-call", "sh", raw)}}
	ctx := context.WithValue(context.Background(), turnKey{}, k.ensureTurn())
	_, err := agent.Run(ctx, p, []tools.Tool{routineSourceShell{run}}, nil, agent.Options{OneRound: true, Approve: approve, RunTool: k.runToolCall})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRoutinePrecheckApprovalPreventsExecution(t *testing.T) {
	for _, action := range []string{"create", "update"} {
		for _, allow := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", action, allow), func(t *testing.T) {
				k, tm, endpoint := routineApprovalFixture(t)
				marker := filepath.Join(k.Bot.Root, "env")
				command := "printf marker > " + precheckQuote(marker)
				if action == "update" {
					if err := tm.Save(link.Routine{ID: "check", Prompt: "p", Cron: "0 * * * *", Enabled: true}); err != nil {
						t.Fatal(err)
					}
				}
				asked := 0
				approve := k.trackApprove(func(ctx context.Context, name, summary string) (bool, error) {
					asked++
					if name != "sh" || agent.ToolCallID(ctx) != "source-call" || !strings.Contains(summary, review.ProtectedSuffix) || !strings.Contains(string(agent.ToolArgs(ctx)), "_xbot_routine") {
						t.Errorf("wrong original approval: %s %s %s", name, summary, agent.ToolArgs(ctx))
					}
					return allow, nil
				})
				runRoutineSource(t, k, approve, `{"command":"\"$XBOT_BIN\" routine create --self --args-file patch.json","description":"configure routine"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
					var reply cliops.RoutineWriteReply
					var err error
					if action == "create" {
						reply, err = cliops.CallRoutineCreate(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.RoutineCreate{ID: "check", Name: "check", Prompt: "p", Cron: "0 * * * *", Precheck: &command})
					} else {
						reply, err = cliops.CallRoutineUpdate(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), "check", cliops.RoutinePatch{Precheck: &command})
					}
					if reply.Applied != allow || (err != nil) == allow {
						t.Errorf("applied=%v allow=%v err=%v", reply.Applied, allow, err)
					}
					return "completed", nil
				})
				if asked != 1 {
					t.Fatalf("approvals=%d", asked)
				}
				r, err := tm.Load("check")
				if allow {
					if err != nil || r.Precheck != command {
						t.Fatalf("approved not saved: %+v %v", r, err)
					}
				} else if action == "create" {
					if !os.IsNotExist(err) {
						t.Fatalf("denied create saved: %+v %v", r, err)
					}
				} else if err != nil || r.Precheck != "" {
					t.Fatalf("denied update changed: %+v %v", r, err)
				}
				if err := tm.Fire(time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC), func(inbox.Event) error { return nil }); err != nil {
					t.Fatal(err)
				}
				_, err = os.Stat(marker)
				if allow && err != nil || !allow && !os.IsNotExist(err) {
					t.Fatalf("marker allow=%v err=%v", allow, err)
				}
			})
		}
	}
}

func TestRoutineCreatePrecheckDuplicateNull(t *testing.T) {
	for _, tc := range []struct {
		name, fields, saved string
		approvals           int
	}{
		{"missing", "", "", 0},
		{"null", `,"precheck":null`, "", 0},
		{"command_then_null", `,"precheck":%s,"precheck":null`, "", 0},
		{"command_then_casefold_null", `,"precheck":%s,"Precheck":null`, "", 0},
		{"command_then_empty", `,"precheck":%s,"precheck":""`, "", 0},
		{"null_then_command", `,"precheck":null,"precheck":%s`, "", 1},
		{"command_null_command", `,"precheck":%s,"precheck":null,"precheck":%[1]s`, "", 1},
		{"single_sensitive_command", `,"precheck":%s`, "", 1},
		{"single_true", `,"precheck":"true"`, "true", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k, tm, endpoint := routineApprovalFixture(t)
			marker := filepath.Join(k.Bot.Root, "env")
			command := "printf marker > " + precheckQuote(marker)
			fields := tc.fields
			if strings.Contains(fields, "%") {
				quoted, _ := json.Marshal(command)
				fields = fmt.Sprintf(fields, string(quoted))
			}
			var in cliops.RoutineCreate
			raw := `{"id":"check","name":"check","prompt":"p","cron":"0 * * * *"` + fields + `}`
			if err := json.Unmarshal([]byte(raw), &in); err != nil {
				t.Fatal(err)
			}
			asked := 0
			runRoutineSource(t, k, k.trackApprove(func(context.Context, string, string) (bool, error) { asked++; return false, nil }), `{"command":"routine create from args file","description":"configure routine"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
				reply, err := cliops.CallRoutineCreate(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), in)
				if (err != nil) != (tc.approvals > 0) || reply.Applied != (tc.approvals == 0) {
					t.Errorf("reply=%+v err=%v", reply, err)
				}
				return "completed", nil
			})
			if asked != tc.approvals {
				t.Fatalf("approvals=%d want=%d", asked, tc.approvals)
			}
			r, err := tm.Load("check")
			if tc.approvals == 0 {
				if err != nil || r.Precheck != tc.saved {
					t.Fatalf("saved=%+v err=%v", r, err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("denied saved: %+v %v", r, err)
			}
			emitted := 0
			now := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
			for i := 0; i < 2; i++ {
				if err := tm.Fire(now, func(inbox.Event) error { emitted++; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			if emitted != 1-tc.approvals {
				t.Fatalf("emitted=%d", emitted)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("unapproved command executed: %v", err)
			}
			if tc.approvals == 0 {
				r, err = tm.Load("check")
				if err != nil || r.LastSlot != "20261027T0800" {
					t.Fatalf("slot=%+v %v", r, err)
				}
			}
		})
	}
}

func precheckQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
