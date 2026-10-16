package kernel

import (
	"testing"

	"xbot/internal/bot"
	"xbot/internal/provider"
)

func callMsg(id, name string) provider.Message {
	return provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
		ID: id, Type: "function", Function: provider.FunctionCall{Name: name, Arguments: "{}"},
	}}}
}

func resultMsg(id string) provider.Message {
	return provider.Message{Role: provider.RoleTool, ToolCallID: id, Content: "ok"}
}

// TestProjectFillsMissingToolResults is the regression for a foreground turn
// whose transcript starts after a background branch's tool call: the projection
// holds the call but the branch's result row is not in the window, and a strict
// OpenAI-compatible gateway rejects the request. Every call must be answered.
func TestProjectFillsMissingToolResults(t *testing.T) {
	entries := []bot.Entry{
		{Message: callMsg("c1", "shell")},
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "继续做"}},
	}
	msgs := project(entries, nil)
	if len(msgs) != 3 {
		t.Fatalf("messages %d, want 3 (call, placeholder result, answer): %+v", len(msgs), msgs)
	}
	if msgs[1].Role != provider.RoleTool || msgs[1].ToolCallID != "c1" {
		t.Fatalf("the call was left unanswered: %+v", msgs)
	}
	if msgs[1].Content == "" {
		t.Fatal("the placeholder result is empty")
	}
}

// TestProjectKeepsRealToolResults checks the placeholder is not added when the
// result is there, and that a result still lands right after its call.
func TestProjectKeepsRealToolResults(t *testing.T) {
	entries := []bot.Entry{
		{Message: callMsg("c1", "shell")},
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "中间一行"}},
		{Message: resultMsg("c1")},
	}
	msgs := project(entries, nil)
	var tools int
	for _, m := range msgs {
		if m.Role == provider.RoleTool {
			tools++
		}
	}
	if tools != 1 {
		t.Fatalf("tool messages %d, want only the real result: %+v", tools, msgs)
	}
	if msgs[1].Role != provider.RoleTool || msgs[1].ToolCallID != "c1" {
		t.Fatalf("the real result did not follow its call: %+v", msgs)
	}
}

// TestStrayToolResultsCountsEachCallOnce checks a second call in the same
// message is filled in too, and that the projection is left alone otherwise.
func TestStrayToolResultsCountsEachCallOnce(t *testing.T) {
	two := provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
		{ID: "c1", Function: provider.FunctionCall{Name: "shell"}},
		{ID: "c2", Function: provider.FunctionCall{Name: "read_file"}},
	}}
	got := strayToolResults([]provider.Message{two, resultMsg("c1")})
	if len(got) != 3 {
		t.Fatalf("messages %d, want 3: %+v", len(got), got)
	}
	var filled, real int
	for _, m := range got {
		if m.Role != provider.RoleTool {
			continue
		}
		switch m.ToolCallID {
		case "c2":
			filled++
		case "c1":
			real++
		}
	}
	if filled != 1 {
		t.Fatalf("the second call was not filled exactly once: %+v", got)
	}
	if real != 1 {
		t.Fatalf("the real result was disturbed: %+v", got)
	}

	// Nothing is added when every call is answered.
	plain := []provider.Message{{Role: provider.RoleAssistant, Content: "好了"}}
	if got := strayToolResults(plain); len(got) != 1 {
		t.Fatalf("messages %d, want 1: %+v", len(got), got)
	}
}
