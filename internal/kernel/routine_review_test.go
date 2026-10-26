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
	"xbot/internal/link"
	"xbot/internal/machine"
	"xbot/internal/provider"
	"xbot/internal/review"
	"xbot/internal/tools"
)

func TestRoutinePrecheckUsesShellReview(t *testing.T) {
	b := testBot(t, link.NameTimer)
	tm := &link.Timer{Dir: b.RoutineDir()}
	mode := machine.Allow
	on := true
	k := &Kernel{Bot: b, Links: []link.Link{tm}, Machine: func() (string, error) { return mode, nil }, PermissionsOn: func() bool { return on }}
	k.ensureTurn().turnTrusted = true
	protected := "printf marker > " + precheckQuote(filepath.Join(b.Root, "env"))
	for _, tc := range []struct {
		name, command, mode string
		on, need, reject    bool
	}{
		{"protected_on", protected, machine.Allow, true, true, false},
		{"protected_off", protected, machine.Allow, false, true, false},
		{"ask", "true", machine.Ask, true, true, false},
		{"off", "true", machine.Off, true, false, true},
		{"delete", "rm -f disposable", machine.Allow, true, true, false},
		{"send", "curl -X POST https://example.invalid", machine.Allow, true, true, false},
		{"delete_permissions_off", "rm -f disposable", machine.Allow, false, false, false},
		{"ordinary", "true", machine.Allow, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mode, on = tc.mode, tc.on
			shellArgs, _ := json.Marshal(map[string]string{"description": "test review", "command": tc.command})
			need, _, err := (reviewedTool{Tool: &tools.Shell{}, k: k}).Confirm(shellArgs)
			if need != tc.need || (err != nil) != tc.reject {
				t.Fatalf("shell reference: need=%v err=%v", need, err)
			}
			for _, tool := range []tools.Tool{routineCreate{k}, routineUpdate{k}} {
				args, _ := json.Marshal(map[string]string{"id": "check", "name": "check", "prompt": "p", "cron": "0 * * * *", "precheck": tc.command})
				got, summary, err := (reviewedTool{Tool: tool, k: k}).Confirm(args)
				if got != tc.need || (err != nil) != tc.reject {
					t.Fatalf("%s bypassed shell: need=%v err=%v", tool.Name(), got, err)
				}
				if tc.need && !strings.Contains(summary, tc.command) && !strings.Contains(summary, "precheck sh -c") {
					t.Fatalf("card lacks command: %q", summary)
				}
				if strings.HasPrefix(tc.name, "protected") && !strings.Contains(summary, review.ProtectedSuffix) {
					t.Fatalf("missing protected-path summary: %s", summary)
				}
			}
		})
	}
	mode, on = machine.Ask, true
	if err := tm.Save(link.Routine{ID: "check", Prompt: "p", Cron: "0 * * * *", Precheck: "true"}); err != nil {
		t.Fatal(err)
	}
	if need, _, err := (reviewedTool{Tool: routineUpdate{k}, k: k}).Confirm(json.RawMessage(`{"id":"check","enabled":true}`)); err != nil || !need {
		t.Fatalf("kept command bypassed review: %v %v", need, err)
	}
	for _, tool := range []tools.Tool{routineCreate{k}, routineUpdate{k}} {
		for _, field := range []string{`,"precheck":""`, `,"precheck":"  \n"`} {
			if need, _, err := (reviewedTool{Tool: tool, k: k}).Confirm(json.RawMessage(`{"id":"check"` + field + `}`)); err != nil || need {
				t.Fatalf("empty precheck changed old scheduling: %v %v", need, err)
			}
		}
	}
	if err := tm.Update("check", func(r *link.Routine) error { r.Precheck = ""; return nil }); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []tools.Tool{routineCreate{k}, routineUpdate{k}} {
		if need, _, err := (reviewedTool{Tool: tool, k: k}).Confirm(json.RawMessage(`{"id":"check","enabled":true}`)); err != nil || need {
			t.Fatalf("ordinary routine now asks: %v %v", need, err)
		}
	}
}

// Review must resolve a relative precheck against its execution cwd even if a
// conversation's shell has cd'ed elsewhere.
func TestRoutinePrecheckReviewsProcessCWD(t *testing.T) {
	b := testBot(t, link.NameTimer)
	t.Chdir(b.Root)
	k := &Kernel{Bot: b, WorkDir: t.TempDir(), Permissive: true}
	for _, tool := range []tools.Tool{routineCreate{k}, routineUpdate{k}} {
		need, summary, err := (reviewedTool{Tool: tool, k: k}).Confirm(json.RawMessage(`{"id":"check","precheck":"printf marker > env"}`))
		if err != nil || !need || !strings.Contains(summary, review.ProtectedSuffix) {
			t.Fatalf("%s: need=%v summary=%q err=%v", tool.Name(), need, summary, err)
		}
	}
}

func TestRoutinePrecheckApprovalPreventsExecution(t *testing.T) {
	for _, name := range []string{"routine_create", "routine_update"} {
		for _, allow := range []bool{false, true} {
			t.Run(name+"/"+map[bool]string{false: "deny", true: "allow"}[allow], func(t *testing.T) {
				b := testBot(t, link.NameTimer)
				tm := &link.Timer{Dir: b.RoutineDir()}
				marker := filepath.Join(b.Root, "env") // Fake protected data, no real credentials.
				command := "printf marker > " + precheckQuote(marker)
				k := &Kernel{Bot: b, Links: []link.Link{tm}}
				k.ensureTurn().turnTrusted = true
				var tool tools.Tool = routineCreate{k}
				if name == "routine_update" {
					tool = routineUpdate{k}
					if err := tm.Save(link.Routine{ID: "check", Prompt: "p", Cron: "0 * * * *", Enabled: true}); err != nil {
						t.Fatal(err)
					}
				}
				args, _ := json.Marshal(map[string]string{"id": "check", "name": "check", "prompt": "p", "cron": "0 * * * *", "precheck": command})
				p := &scripted{resps: []provider.Message{toolCall("check-call", name, string(args))}}
				asked := 0
				_, err := agent.Run(context.Background(), p, []tools.Tool{reviewedTool{Tool: tool, k: k}}, nil, agent.Options{OneRound: true, Approve: func(_ context.Context, got, summary string) (bool, error) {
					asked++
					if got != name || !strings.Contains(summary, review.ProtectedSuffix) {
						t.Errorf("approval %s %q", got, summary)
					}
					return allow, nil
				}})
				if err != nil || asked != 1 {
					t.Fatalf("approval count=%d err=%v", asked, err)
				}
				r, err := tm.Load("check")
				if allow {
					if err != nil || r.Precheck != command {
						t.Fatalf("approved command not saved: %+v %v", r, err)
					}
				} else if name == "routine_create" {
					if !os.IsNotExist(err) {
						t.Fatalf("denied create persisted: %+v %v", r, err)
					}
				} else if err != nil || r.Precheck != "" {
					t.Fatalf("denied update changed command: %+v %v", r, err)
				}
				if err := tm.Fire(time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC), func(link.Event) error { return nil }); err != nil {
					t.Fatal(err)
				}
				_, err = os.Stat(marker)
				if allow && err != nil || !allow && !os.IsNotExist(err) {
					t.Fatalf("protected write after allow=%v: %v", allow, err)
				}
			})
		}
	}
}

func precheckQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
