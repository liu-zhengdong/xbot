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
