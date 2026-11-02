package eval

import (
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/provider"
)

func TestRecallResultRequired(t *testing.T) {
	cases, _, err := LoadSuite("", "proactivity")
	if err != nil {
		t.Fatal(err)
	}
	var c Case
	for _, one := range cases {
		if one.Name == "memory-recall" {
			c = one
		}
	}
	if c.Name == "" {
		t.Fatal("memory-recall scenario missing")
	}
	script, ok := passing(c.Name)
	if !ok || len(script) == 0 || script[0].Tool != "sh" {
		t.Fatal("memory-recall driver must issue its actual source CLI")
	}
	call := provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
		ID: "actual_recall", Type: "function", Function: provider.FunctionCall{Name: script[0].Tool, Arguments: script[0].Args},
	}})}
	for _, result := range []string{"no matches", "violet-orbit-73", "bot:projects/x.md:1", "violet-orbit-73 bot:projects/x.md:1"} {
		t.Run(result, func(t *testing.T) {
			obs := Observation{
				Assistant: "violet-orbit-73 projects/x.md",
				Tools:     []ToolUse{{Name: script[0].Tool, Args: script[0].Args}},
				entries: []bot.Entry{
					{Message: call},
					{Message: provider.Message{Role: provider.RoleTool, Name: "sh", Content: provider.ToolResult("actual_recall", result)}},
				},
			}
			problems, _ := grade(c, obs, nil, t.TempDir(), nil)
			wantPass := result == "violet-orbit-73 bot:projects/x.md:1"
			if (len(problems) == 0) != wantPass {
				t.Fatalf("result %q: %v", result, problems)
			}
		})
	}
}

func TestMemoryRecallCaseResultMatchesActualCall(t *testing.T) {
	const command = `"$XBOT_BIN" memory-recall --self --query 'launch code' --scope bot`
	const full = "- bot:projects/x.md:1: The launch code for project X is violet-orbit-73."
	parts := []string{"violet-orbit-73", "bot:projects/x.md:1"}
	call := func(id, cmd string) bot.Entry {
		return bot.Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
			ID: id, Type: "function", Function: provider.FunctionCall{Name: "sh", Arguments: args(map[string]string{"command": cmd})},
		}})}}
	}
	result := func(id, body string) bot.Entry {
		return bot.Entry{Message: provider.Message{Role: provider.RoleTool, Name: "sh", Content: provider.ToolResult(id, body)}}
	}
	for _, tc := range []struct {
		name    string
		entries []bot.Entry
		want    bool
	}{
		{"canonical", []bot.Entry{call("r", command), result("r", full)}, true},
		{"legacy_history", []bot.Entry{{Message: provider.Message{Role: provider.RoleTool, Name: "recall", Content: provider.TextContent(full)}}}, true},
		{"unrelated_shell_fake_output", []bot.Entry{call("r", "printf '"+full+"'"), result("r", full)}, false},
		{"wrong_query", []bot.Entry{call("r", strings.Replace(command, "launch code", "tea", 1)), result("r", full)}, false},
		{"wrong_scope", []bot.Entry{call("r", strings.Replace(command, "--scope bot", "--scope person", 1)), result("r", full)}, false},
		{"missing_self", []bot.Entry{call("r", strings.Replace(command, " --self", "", 1)), result("r", full)}, false},
		{"extra_since", []bot.Entry{call("r", command+" --since 2026-10-18"), result("r", full)}, false},
		{"query_file", []bot.Entry{call("r", `"$XBOT_BIN" memory-recall --self --query-file query.txt --scope bot`), result("r", full)}, false},
		{"help", []bot.Entry{call("r", `"$XBOT_BIN" memory-recall --help`), result("r", full)}, false},
		{"chained_command", []bot.Entry{call("r", command+"; printf fake"), result("r", full)}, false},
		{"other_call_id", []bot.Entry{call("r", command), result("other", full)}, false},
		{"orphan_result", []bot.Entry{result("r", full)}, false},
		{"result_before_call", []bot.Entry{result("r", full), call("r", command)}, false},
		{"reused_call_id", []bot.Entry{call("r", command), call("r", "printf fake"), result("r", full)}, false},
		{"only_answer", []bot.Entry{call("r", command), result("r", parts[0])}, false},
		{"only_path", []bot.Entry{call("r", command), result("r", parts[1])}, false},
		{"split_results", []bot.Entry{call("r1", command), result("r1", parts[0]), call("r2", command), result("r2", parts[1])}, false},
		{"artifact_path_only", []bot.Entry{call("r", command), result("r", `{"file":"results/recall.json","access":"local","bytes":40000}`)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := memoryRecallCaseResult(tc.entries, parts); got != tc.want {
				t.Fatalf("actual recall evidence = %v, want %v", got, tc.want)
			}
		})
	}
}
