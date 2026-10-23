package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"xbot/internal/agent"
	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

func TestEnvironmentReceiptSurvivesBlobAndExternalUndo(t *testing.T) {
	k, _ := envBaselineKernel(t)
	var text strings.Builder
	for i := 0; i < 24; i++ {
		text.WriteString("- (2026-10-22) actual fact " + strings.Repeat("x", i) + "\n")
	}
	call := memoryWriteCall(k.Bot, "large-receipt", memory.ScopeBot, "unused")
	var args map[string]string
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		t.Fatal(err)
	}
	args["content"] = text.String()
	raw, _ := json.Marshal(args)
	call.Function.Arguments = string(raw)
	out, err := (reviewedTool{Tool: &tools.WriteFile{}, k: k}).Run(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise storage with a large ordinary body and a complete receipt.
	out = strings.Repeat("execution detail ", 400) + out
	if len(out) <= 4096 {
		t.Fatal("test did not spill a large result")
	}
	if err := k.Bot.Append(bot.Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{call})}}); err != nil {
		t.Fatal(err)
	}
	if err := k.Bot.Append(bot.Entry{Message: provider.Message{Role: provider.RoleTool, Name: "write_file", Content: provider.ToolResult(call.ID, out)}}); err != nil {
		t.Fatal(err)
	}
	rows, err := k.Bot.Entries()
	if err != nil {
		t.Fatal(err)
	}
	saved := rows[len(rows)-1]
	if saved.Blob == "" {
		t.Fatal("test did not spill the tool result")
	}
	live, err := k.memoryText(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	seen := testPublished(t, k)
	if changedMemory(seen.Memory, memoryFacts(live)) != nil {
		t.Fatal("blob discarded a successful own-write receipt")
	}
	// Someone reverts it before the next wake. Replaying the original receipt
	// is essential: current memory now equals the frozen prefix again.
	if _, err := (&tools.WriteFile{}).Run(context.Background(), mustArgsRaw(t, map[string]string{"path": args["path"], "content": ""})); err != nil {
		t.Fatal(err)
	}
	st, err := k.Bot.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.syncEnv(st, link.Event{}, ""); err != nil {
		t.Fatal(err)
	}
	p, ok := decodeEnvironment(lastEnvUpdate(t, k))
	if !ok || p.Memory == nil || len(p.Memory.Remove) == 0 {
		t.Fatal("external reversal of spilled own write disappeared")
	}
}
func mustArgsRaw(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestEnvironmentReceiptSurvivesAttachedReminder(t *testing.T) {
	p := environmentMessage{Memory: &memoryChange{Add: []string{"[Bot] ORIGINAL_WRITE"}}}
	receipt := encodeEnvironment(p)
	messages := []provider.Message{
		{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{ID: "write", Function: provider.FunctionCall{Name: "write_file"}}})},
		{Role: provider.RoleTool, Name: "write_file", Content: provider.ToolResult("write", "wrote\n\n"+receipt)},
	}
	messages = agent.AttachReminder(messages, "Keep the current todo in mind.")
	seen := replayEnvironment(messages, "")
	if len(seen.Memory) != 1 {
		t.Fatal("visible receipt ignored because a reminder follows it")
	}
	undo := changedMemory(seen.Memory, nil)
	if undo == nil || len(undo.Remove) != 1 {
		t.Fatal("reversal after reminder was hidden")
	}
}
func TestEnvironmentOlderFitCannotOverwriteNewSender(t *testing.T) {
	k, _ := envBaselineKernel(t)
	st, err := k.Bot.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	rawMessages, _, err := k.assemble(st, st.MemSnap)
	if err != nil {
		t.Fatal(err)
	}
	raw := provider.Request{Messages: rawMessages}
	// New sender arrives after prepare, before the old generation finishes fit.
	if _, err := k.receive(link.TerminalEvent("u2", "bob speaks during fit", time.Unix(1001, 0))); err != nil {
		t.Fatal(err)
	}
	folded := provider.Request{Messages: []provider.Message{turnSummary("old A context", 1, 2)}}
	r := &turnRun{k: k}
	sent, err := r.finishEnvironment(context.Background(), raw, folded)
	if err != nil {
		t.Fatal(err)
	}
	got := replayEnvironment(sent.Messages, st.MemSnap)
	if source := environmentGet[environmentSource](got, "source"); source.Sender != "u2" {
		t.Fatalf("old request overwrote current sender: %+v", source)
	}
	if !strings.Contains(environmentGet[string](got, "person:u2"), "BOB_BASELINE_47") {
		t.Fatal("snapshot did not sample current person's facts")
	}
	before, _ := k.Bot.Count()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.finishEnvironment(canceled, raw, folded); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled finalizer: %v", err)
	}
	after, _ := k.Bot.Count()
	if after != before {
		t.Fatal("canceled request appended environment")
	}
}
