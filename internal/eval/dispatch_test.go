package eval

import (
	"strings"
	"testing"
)

// The dispatch suite runs without the jail, so it runs on every platform,
// and its passing scripts pass.
func TestDispatchSuitePassesWithScripts(t *testing.T) {
	cases, from, err := LoadSuite("", "dispatch")
	if err != nil || from != "embedded" || len(cases) < 4 {
		t.Fatalf("from %s n %d err %v", from, len(cases), err)
	}
	if err := jailFor(Options{}, []string{"dispatch"}); err != nil {
		t.Fatalf("dispatch suite wants the jail: %v", err)
	}
	for _, c := range cases {
		if !c.Dispatch {
			t.Fatalf("%s is not a dispatch case", c.Name)
		}
		script, ok := passing(c.Name)
		if !ok {
			t.Fatalf("no script for %s", c.Name)
		}
		res := runCase(c, "normal", script, nil)
		if !res.Pass {
			t.Fatalf("%s: %s\n%s", c.Name, res.Reason, res.Transcript)
		}
	}
}

// The grader fails a wrong type, a task that leaves out what the subagent
// needs, and a dispatch for something answered on the spot.
func TestGradeDispatch(t *testing.T) {
	want := &DispatchCheck{Type: "dev", TaskMentions: []string{"cart.go|Total", "99"}}
	call := func(ty, task string) []ToolUse {
		return []ToolUse{{Name: "dispatch", Args: `{"type":"` + ty + `","task":"` + task + `"}`}}
	}
	if p := gradeDispatch(want, call("dev", "fix Total in cart.go: free over 99")); len(p) != 0 {
		t.Fatalf("good dispatch failed: %v", p)
	}
	if p := gradeDispatch(want, call("research", "fix Total, free over 99")); len(p) != 1 || !strings.Contains(p[0], "want \"dev\"") {
		t.Fatalf("wrong type: %v", p)
	}
	if p := gradeDispatch(want, call("dev", "fix the cart")); len(p) != 2 {
		t.Fatalf("thin task: %v", p)
	}
	if p := gradeDispatch(want, nil); len(p) != 1 || p[0] != "no dispatch" {
		t.Fatalf("no dispatch: %v", p)
	}
	if p := gradeDispatch(&DispatchCheck{None: true}, call("dev", "x")); len(p) != 1 {
		t.Fatalf("needless dispatch: %v", p)
	}
}

// The new cases must reject changing/cancelling A for an unrelated B, and
// dispatching a new agent for a modification that belongs to A.
func TestBusyDispatchCasesRejectWrongActions(t *testing.T) {
	cases, _, err := LoadSuite("", "dispatch")
	if err != nil {
		t.Fatal(err)
	}
	bad := map[string]Script{
		"dispatch-busy-new-task": {
			{Tool: "cancel", Args: `{"id":"sa_0123456789abcdef"}`},
			{Tool: "dispatch", Args: `{"type":"research","task":"Go 1.24 changes with source links"}`},
			{Text: "已派出调研。"},
		},
		"dispatch-busy-same-repo": {
			{Tool: "steer", Args: `{"id":"sa_0123456789abcdef","text":"加 docs/setup.md"}`},
			{Tool: "dispatch", Args: `{"type":"dev","task":"In shop add docs/setup.md with local install and start steps"}`},
			{Text: "已派出另一项。"},
		},
		"dispatch-busy-steer": {
			{Tool: "steer", Args: `{"id":"sa_0123456789abcdef","text":"README 改用 go test ./..."}`},
			{Tool: "dispatch", Args: `{"type":"dev","task":"修改 shop/README.md"}`},
			{Text: "追加后又起了一个。"},
		},
	}
	n := 0
	for _, c := range cases {
		if script, ok := bad[c.Name]; ok {
			n++
			r := runCase(c, "normal", script, nil)
			if r.Pass {
				t.Errorf("%s accepted wrong action", c.Name)
			}
			// Removing just the relevant guard must make this otherwise
			// correct dispatch/steer pass; a missing required action must
			// not accidentally be what makes the negative test red.
			cleared := c
			cleared.Checks.ForbidTools = nil
			if c.Name == "dispatch-busy-steer" {
				cleared.Checks.Dispatch = nil
			}
			if unguarded := runCase(cleared, "normal", script, nil); !unguarded.Pass {
				t.Errorf("%s negative fails for an unrelated reason: %s", c.Name, unguarded.Reason)
			}
		}
	}
	if n != 3 {
		t.Fatalf("found %d busy cases, want 3", n)
	}
}

func TestBusyDispatchSeedCanBeInspectedBeforeSteering(t *testing.T) {
	cases, _, err := LoadSuite("", "dispatch")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if c.Name == "dispatch-busy-steer" {
			c.Checks.ToolResultContains["read_subagent"] = []string{"status: running", "id: sa_0123456789abcdef"}
			r := runCase(c, "normal", Script{{Tool: "read_subagent", Args: `{"id":"sa_0123456789abcdef"}`}, {Tool: "steer", Args: `{"id":"sa_0123456789abcdef","text":"README 改用 go test ./..."}`}, {Text: "补充已交给原来的任务。"}}, nil)
			if !r.Pass {
				t.Fatalf("seed is not inspectable: %s\n%s", r.Reason, r.Transcript)
			}
			return
		}
	}
	t.Fatal("missing steer case")
}
