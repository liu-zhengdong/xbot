package kernel

import (
	"testing"

	"xbot/internal/bot"
	"xbot/internal/provider"
)

// A crash right after a fork marker, with a tool call still open, must not wedge repair.
func TestUnresolvedTailSkipsForkMark(t *testing.T) {
	entries := []bot.Entry{
		{Message: provider.Message{Role: provider.RoleUser, Content: "go"}},
		{Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "shell", Arguments: "{}"}}}}},
		{Message: provider.Message{Role: provider.RoleUser}, Mark: &bot.ForkMark{Kind: bot.ForkKindStart, Branch: "bg_1", From: 1, Task: "t"}},
	}
	if _, err := unresolvedTail(entries); err != nil {
		t.Fatal(err)
	}
}

// A result repaired for a call made on a background branch carries that branch.
func TestCallBranchesFollowFork(t *testing.T) {
	call := provider.ToolCall{ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "shell", Arguments: "{}"}}
	span := []bot.Entry{
		{Message: provider.Message{Role: provider.RoleUser, Content: "go"}},
		{Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{call}}},
		{Message: provider.Message{Role: provider.RoleUser}, Mark: &bot.ForkMark{Kind: bot.ForkKindStart, Branch: "bg_1", From: 10, Task: "t"}},
		{Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c3", Type: "function", Function: provider.FunctionCall{Name: "shell", Arguments: "{}"}}}}},
	}
	_, got, err := unresolvedCalls(span, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got["c1"] != "bg_1" || got["c3"] != "" {
		t.Fatalf("branches %v", got)
	}
}

func call(id string) []provider.ToolCall {
	return []provider.ToolCall{{ID: id, Type: "function", Function: provider.FunctionCall{Name: "shell", Arguments: "{}"}}}
}

// Open calls in the middle of the file, one per branch, are each repaired on their own branch.
func TestUnresolvedAcrossBranches(t *testing.T) {
	span := []bot.Entry{
		{Message: provider.Message{Role: provider.RoleUser, Content: "go"}},                                                                    // 1
		{Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: call("a")}},                                                        // 2 bg_1 (pre-fork)
		{Message: provider.Message{Role: provider.RoleUser}, Mark: &bot.ForkMark{Kind: bot.ForkKindStart, Branch: "bg_1", From: 1, Task: "t"}}, // 3
		{Message: provider.Message{Role: provider.RoleUser, Content: "hi"}},                                                                    // 4 main
		{Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: call("b")}},                                                        // 5 main -> bg_2
		{Message: provider.Message{Role: provider.RoleUser}, Mark: &bot.ForkMark{Kind: bot.ForkKindStart, Branch: "bg_2", From: 4, Task: "u"}}, // 6
		{Message: provider.Message{Role: provider.RoleUser, Content: "more"}},                                                                  // 7 main
		{Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: call("c")}},                                                        // 8 main
	}
	calls, br, err := unresolvedCalls(span, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 || br["a"] != "bg_1" || br["b"] != "bg_2" || br["c"] != "" {
		t.Fatalf("calls %d branches %v", len(calls), br)
	}
}

// A call followed by later rows of its own branch cannot be repaired.
func TestUnresolvedLaterRowsInBranchErrors(t *testing.T) {
	span := []bot.Entry{
		{Message: provider.Message{Role: provider.RoleUser, Content: "go"}},
		{Branch: "bg_1", Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: call("a")}},
		{Message: provider.Message{Role: provider.RoleUser, Content: "hi"}},
		{Branch: "bg_1", Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}},
	}
	if _, _, err := unresolvedCalls(span, 1); err == nil {
		t.Fatal("want error")
	}
}
