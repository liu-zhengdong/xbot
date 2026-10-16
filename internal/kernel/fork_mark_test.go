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
		{Branch: "bg_1", Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c2", Type: "function", Function: provider.FunctionCall{Name: "shell", Arguments: "{}"}}}}},
		{Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c3", Type: "function", Function: provider.FunctionCall{Name: "shell", Arguments: "{}"}}}}},
	}
	got := callBranches(span, 10)
	if got["c1"] != "bg_1" || got["c2"] != "bg_1" || got["c3"] != "" {
		t.Fatalf("branches %v", got)
	}
}
