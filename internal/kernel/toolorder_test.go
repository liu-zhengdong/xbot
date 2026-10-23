package kernel

import (
	"testing"

	"xbot/internal/bot"
	"xbot/internal/provider"
)

// A turn that moves to the background mid-call leaves the cap notice (and later
// owner messages) between the assistant tool_calls row and its results. Strict
// gateways answer "tool calls and tool results do not match". The projection
// must put every result right after its call.
func TestProjectionGroupsToolResultsAfterFork(t *testing.T) {
	call := func(ids ...string) provider.Message {
		m := provider.Message{Role: provider.RoleAssistant}
		for _, id := range ids {
			m.SetCalls(append(m.Calls(), provider.ToolCall{ID: id}))
		}
		return m
	}
	res := func(id string) provider.Message {
		return provider.Message{Role: provider.RoleTool, Content: provider.ToolResult(id, "ok "+id)}
	}
	entries := []bot.Entry{
		{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("task")}},
		{Branch: "bg_1", Message: call("a", "b")},
		{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("这件事比较长，已转到后台 bg_1 继续做，做完告诉你。")}},
		{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("owner again")}},
		{Branch: "bg_1", Message: res("a")},
		{Branch: "bg_1", Message: res("b")},
		{Branch: "bg_1", Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("done")}},
	}
	msgs := project(entries, nil)
	for i, m := range msgs {
		if len(m.Calls()) == 0 {
			continue
		}
		for j := range m.Calls() {
			if k := i + 1 + j; k >= len(msgs) || msgs[k].Role != provider.RoleTool {
				t.Fatalf("message %d after tool_calls at %d is %q, want a tool result; msgs=%+v", k, i, msgs[k].Role, msgs)
			}
		}
	}
	if len(msgs) != len(entries) {
		t.Fatalf("got %d messages, want %d", len(msgs), len(entries))
	}
	if msgs[4].Text() != "这件事比较长，已转到后台 bg_1 继续做，做完告诉你。" || msgs[5].Text() != "owner again" {
		t.Fatalf("rows between call and results lost their order: %+v", msgs)
	}
}

// A gateway that omits tool call ids got "call_local_1" in every turn. With a
// repeated id the projection paired the first result with the last call and
// left the earlier call without a result (dev bot, 2026-10-17 01:5x).
func TestProjectionPairsRepeatedCallIDsWithNearestCall(t *testing.T) {
	call := provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{ID: "call_local_1"}})}
	res := func(c string) provider.Message {
		return provider.Message{Role: provider.RoleTool, Content: provider.ToolResult("call_local_1", c)}
	}
	entries := []bot.Entry{
		{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("one")}},
		{Message: call},
		{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("这件事比较长，已转到后台 bg_1 继续做，做完告诉你。")}},
		{Branch: "bg_1", Message: res("first")},
		{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("two")}},
		{Message: call},
		{Message: res("second")},
	}
	msgs := project(entries, nil)
	if len(msgs) != len(entries) {
		t.Fatalf("got %d messages, want %d", len(msgs), len(entries))
	}
	if msgs[2].Text() != "first" || msgs[6].Text() != "second" {
		t.Fatalf("results not paired with their nearest call: %+v", msgs)
	}
}
