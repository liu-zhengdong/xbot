package eval

import (
	"strings"
	"testing"
)

func TestNoToolCallsGradesObservedCalls(t *testing.T) {
	for _, tc := range []struct {
		name   string
		checks Checks
		tools  []ToolUse
		pass   bool
	}{
		{"direct answer", Checks{NoToolCalls: true}, nil, true},
		{"unnecessary read", Checks{NoToolCalls: true}, []ToolUse{{Name: "read_file", Args: `{"path":"project.txt"}`}}, false},
		{"attempted call", Checks{NoToolCalls: true}, []ToolUse{{Name: "unknown_tool", Args: `{}`}}, false},
		{"unset maximum stays unlimited", Checks{}, []ToolUse{{Name: "read_file", Args: `{"path":"project.txt"}`}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			problems, _ := grade(Case{Checks: tc.checks}, Observation{Tools: tc.tools}, nil, t.TempDir(), nil)
			if (len(problems) == 0) != tc.pass {
				t.Fatalf("pass=%v problems=%v", tc.pass, problems)
			}
			for _, p := range problems {
				if ruleViolation(p) {
					t.Fatal("unnecessary calls are an ability failure, not a safety veto")
				}
			}
		})
	}
}

func TestNoToolCallsYAML(t *testing.T) {
	raw := "name: direct\nfile_only: true\nwake:\n  kind: user\n  text: '{{workdir}} {{files}}'\nchecks:\n  no_tool_calls: true\n"
	c, err := decodeCase("direct.yaml", []byte(raw), nil)
	if err != nil || !c.Checks.NoToolCalls {
		t.Fatalf("decode: %+v %v", c.Checks, err)
	}
	if _, err := decodeCase("direct.yaml", []byte(raw+"  min_tool_calls: 1\n"), nil); err == nil {
		t.Fatal("accepted contradictory tool requirements")
	}
}

func TestNextPromptCasesObserveWork(t *testing.T) {
	for _, name := range []string{"knowledge-public", "knowledge-project", "knowledge-fresh", "ambiguous-start", "desktop-complete", "chinese-opening"} {
		t.Run(name, func(t *testing.T) {
			c := mustCase(t, name)
			s, ok := passing(name)
			if !ok || !c.FileOnly {
				t.Fatal("missing scripted behavior or shell-free fixture")
			}
			res := runCase(c, "normal", s, nil)
			if !res.Pass {
				t.Fatalf("positive: %s", res.Reason)
			}
			bad := Script{{Text: "已经完成。"}}
			if name == "knowledge-public" {
				bad = append(Script{{Tool: "read_file", Args: `{"path":"project.txt"}`}}, s...)
			}
			res = runCase(c, "normal", bad, nil)
			if res.Pass {
				t.Fatal("accepted claim without required evidence (or unnecessary tool)")
			}
		})
	}
}

func TestDesktopRequiresCompanionsAndOptIn(t *testing.T) {
	c := mustCase(t, "desktop-complete")
	for _, config := range []string{
		`{"window":{"url":"http://127.0.0.1:4312"}}`,
		`{"window":{"url":"http://127.0.0.1:4312","close":"hide"},"package":{"installer":true},"tray":{"enabled":true},"login":{"option":true,"enabled":true}}`,
	} {
		s := Script{
			{Tool: "read_file", Args: `{"path":"desktop.json"}`},
			{Tool: "write_file", Args: args(map[string]string{"path": "desktop.json", "content": config})},
			{Text: "桌面端已完成。"},
		}
		// Even an overly generous semantic judge cannot override file checks.
		judge := &captureJudge{fake: &Fake{Steps: s}, verdict: `{"pass":true,"reason":"claims completion"}`}
		res := runCase(c, "normal", nil, judge)
		if res.Pass || !strings.Contains(res.Reason, "JSON field") {
			t.Fatalf("partial or opt-out app passed: %s", res.Reason)
		}
	}
}

func TestChineseOpeningJudgeSeesPreamble(t *testing.T) {
	c := mustCase(t, "chinese-opening")
	s, _ := passing(c.Name)
	s = append(Script(nil), s...)
	s[0].Text = "I'll inspect the configuration and update it."
	judge := &captureJudge{fake: &Fake{Steps: s}, verdict: `{"pass":false,"reason":"English preamble"}`}
	res := runCase(c, "normal", nil, judge)
	if res.Pass || !strings.Contains(judge.prompt, s[0].Text) || !strings.Contains(judge.prompt, "标题已改成") {
		t.Fatalf("judge did not see both preamble and final reply: %s", res.Reason)
	}
}
