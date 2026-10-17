package kernel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/config"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

type pingLink struct{}

func (pingLink) Name() string        { return "pinglink" }
func (pingLink) Tools() []tools.Tool { return []tools.Tool{pingTool{}} }

type pingTool struct{}

func (pingTool) Name() string        { return "ping" }
func (pingTool) Description() string { return "a no-op step" }
func (pingTool) Parameters() any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (pingTool) Confirm(json.RawMessage) (bool, string, error) { return false, "ping", nil }
func (pingTool) Run(context.Context, json.RawMessage) (string, error) {
	return "pong", nil
}

type roLink struct{}

func (roLink) Name() string        { return "rolink" }
func (roLink) Tools() []tools.Tool { return []tools.Tool{roTool{}} }

type roTool struct{}

func (roTool) Name() string        { return "read_file" }
func (roTool) Description() string { return "read" }
func (roTool) Parameters() any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (roTool) Confirm(json.RawMessage) (bool, string, error) { return false, "read", nil }
func (roTool) Run(context.Context, json.RawMessage) (string, error) {
	return "ok", nil
}

func pingCall(id string) provider.Message {
	return provider.Message{
		Role: provider.RoleAssistant,
		ToolCalls: []provider.ToolCall{{
			ID: id, Type: "function",
			Function: provider.FunctionCall{Name: "ping", Arguments: "{}"},
		}},
	}
}

func todoCall(id, args string) provider.Message {
	return provider.Message{
		Role: provider.RoleAssistant,
		ToolCalls: []provider.ToolCall{{
			ID: id, Type: "function",
			Function: provider.FunctionCall{Name: "todo", Arguments: args},
		}},
	}
}

func lastContent(req provider.Request) string {
	if len(req.Messages) == 0 {
		return ""
	}
	return req.Messages[len(req.Messages)-1].Content
}

func TestTodoWriteRulesAndDescription(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, Redact: func(s string) string {
		return strings.ReplaceAll(s, "SECRETVALUE", "[secret]")
	}}
	tool := todoTool{k}
	if !strings.Contains(tool.Description(), "3 or more") || !strings.Contains(tool.Description(), "simple question") {
		t.Fatalf("description should say when to use it and when not:\n%s", tool.Description())
	}
	good := `{"items":[{"content":"write the tests","status":"in_progress"},{"id":"docs","content":"update the docs SECRETVALUE","status":"pending"},{"content":"skip","status":"dropped","reason":"not needed"}]}`
	out, err := tool.Run(context.Background(), json.RawMessage(good))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "SECRETVALUE") {
		t.Fatalf("tool result kept a secret: %s", out)
	}
	raw, err := os.ReadFile(TodoPath(b))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "SECRETVALUE") {
		t.Fatalf("list file kept a secret:\n%s", raw)
	}
	info, err := os.Stat(TodoPath(b))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	if filepath.Dir(TodoPath(b)) != bot.Dir(b.Root, b.Name()) {
		t.Fatalf("list is not in the bot directory: %s", TodoPath(b))
	}
	items, err := ReadTodos(b)
	if err != nil || len(items) != 3 || items[0].ID != "t1" || items[0].Status != "in_progress" || items[1].Content != "update the docs [secret]" {
		t.Fatalf("%+v %v", items, err)
	}
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"items":[{"content":"a","status":"in_progress"},{"content":"b","status":"in_progress"}]}`)); err == nil {
		t.Fatal("two in_progress items were accepted")
	}
	items, _ = ReadTodos(b)
	if len(items) != 3 || items[0].Status != "in_progress" {
		t.Fatalf("rejected write changed the list: %+v", items)
	}
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"items":[{"content":"a","status":"dropped"}]}`)); err == nil {
		t.Fatal("dropped without a reason was accepted")
	}
	sum := TodoSummary(b)
	if !strings.Contains(sum, "write the tests") || strings.Contains(sum, "SECRETVALUE") {
		t.Fatalf("summary %q", sum)
	}
}

func TestTodoReminderDiffCooldownAndNotAfterUpdate(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	t0 := time.Unix(1_700_000_000, 0).UTC()
	now := t0
	list := `{"items":[{"id":"t1","content":"write the tests","status":"in_progress"},{"id":"t2","content":"update the docs","status":"pending"},{"id":"t3","content":"already finished","status":"done"}]}`
	updated := `{"items":[{"id":"t1","content":"write the tests","status":"done"},{"id":"t2","content":"update the docs","status":"pending"},{"id":"t3","content":"already finished","status":"done"}]}`
	p := &scripted{resps: []provider.Message{
		todoCall("c0", list),
		pingCall("c1"),
		pingCall("c2"),
		todoCall("c3", updated),
		pingCall("c4"),
		pingCall("c5"),
		{Role: provider.RoleAssistant, Content: "ok"},
		{Role: provider.RoleAssistant, Content: "ok2"},
	}}
	k := &Kernel{
		Bot: b, Provider: p, MaxSteps: 12, TodoSteps: 2,
		Links: []link.Link{link.Terminal{}, pingLink{}},
		Now:   func() time.Time { return now },
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "start", t0)); err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) != 8 {
		t.Fatalf("requests %d", len(p.reqs))
	}
	if strings.Contains(lastContent(p.reqs[1]), "<todo_reminder>") {
		t.Fatal("reminder fired on the step right after the update")
	}
	first := lastContent(p.reqs[3])
	if !strings.Contains(first, "<todo_reminder>") || !strings.Contains(first, "write the tests") || !strings.Contains(first, "update the docs") {
		t.Fatalf("first reminder missing open items: %s", first)
	}
	if strings.Contains(first, "already finished") {
		t.Fatal("first reminder repeated a finished item")
	}
	if strings.Contains(lastContent(p.reqs[6]), "in_progress -> done") {
		t.Fatal("cooldown did not hold the diff back")
	}
	gate := p.reqs[7]
	gateMsg := gate.Messages[len(gate.Messages)-1]
	if gateMsg.Role != provider.RoleUser || !strings.Contains(gateMsg.Content, "<todo_gate>") {
		t.Fatalf("gate should be its own user message: role=%s content=%s", gateMsg.Role, gateMsg.Content)
	}
	if prev := gate.Messages[len(gate.Messages)-2]; strings.Contains(prev.Content, "<todo_gate>") || prev.Role != provider.RoleAssistant {
		t.Fatalf("gate was glued onto the assistant message: role=%s content=%s", prev.Role, prev.Content)
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Message.Content, "<todo_reminder>") {
			t.Fatal("reminder was written into the transcript")
		}
		if strings.Contains(e.Message.Content, "<todo_gate>") && e.Message.Role != provider.RoleUser {
			t.Fatalf("gate attributed to %s", e.Message.Role)
		}
	}
	now = t0.Add(todoCooldown + time.Second)
	diff := k.todoNote(now, k.todoPrev())
	if !strings.Contains(diff, "in_progress -> done") || !strings.Contains(diff, "write the tests") {
		t.Fatalf("diff reminder: %s", diff)
	}
	if strings.Contains(diff, "update the docs") {
		t.Fatal("diff repeated an item that did not change")
	}
	if _, steps := k.todoPolicy(); steps != 2 {
		t.Fatalf("configured steps %d", steps)
	}
}

func TestTodoDefaultStepsAndModelSwitch(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b}
	on, steps := k.todoPolicy()
	if !on || steps != defaultTodoStaleSteps || defaultTodoStaleSteps != 6 {
		t.Fatalf("default on=%v steps=%d", on, steps)
	}
	if err := b.SetBinding("openai", "gpt-4"); err != nil {
		t.Fatal(err)
	}
	offK := &Kernel{Bot: b, Links: []link.Link{link.Terminal{}}}
	on, _ = offK.todoPolicy()
	if on {
		t.Fatal("gpt-4 should default the reminder off")
	}
	list, err := offK.modelTools()
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range list {
		if tool.Name() == "todo" {
			t.Fatal("todo tool is offered when the switch is off")
		}
	}

	root := b.Root
	off := false
	if err := config.Add(root, config.Provider{
		Name: "deepseek", BaseURL: "http://127.0.0.1:9/v1", APIKeyEnv: "XBOT_TEST_UNUSED",
	}, config.ModelSpec{Name: "deepseek-chat", Context: 1024, Todo: &off, TodoSteps: 4}); err != nil {
		t.Fatal(err)
	}
	if err := b.SetBinding("deepseek", "deepseek-chat"); err != nil {
		t.Fatal(err)
	}
	deep := &Kernel{Bot: b}
	on, steps = deep.todoPolicy()
	if on || steps != 4 {
		t.Fatalf("explicit off on=%v steps=%d", on, steps)
	}
	yes := true
	if err := config.Add(root, config.Provider{
		Name: "openai", BaseURL: "http://127.0.0.1:9/v1", APIKeyEnv: "XBOT_TEST_UNUSED",
	}, config.ModelSpec{Name: "gpt-4", Context: 1024, Todo: &yes}); err != nil {
		t.Fatal(err)
	}
	if err := b.SetBinding("openai", "gpt-4"); err != nil {
		t.Fatal(err)
	}
	gpt := &Kernel{Bot: b}
	on, _ = gpt.todoPolicy()
	if !on {
		t.Fatal("explicit todo:true should turn a non-deepseek model on")
	}
}

func TestTodoQuietForReadOnly(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	p := &scripted{resps: []provider.Message{
		todoCall("c0", `{"items":[{"content":"write the tests","status":"pending"}]}`),
		{Role: provider.RoleAssistant, Content: "ok"},
		{Role: provider.RoleAssistant, Content: "should not run"},
	}}
	ro := &Kernel{
		Bot: b, Provider: p, MaxSteps: 4,
		Links: []link.Link{roLink{}},
	}
	if err := ro.Handle(context.Background(), link.TerminalEvent("u1", "look", time.Unix(11, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) != 2 {
		t.Fatalf("read-only continued: %d", len(p.reqs))
	}
	for _, r := range p.reqs {
		for _, m := range r.Messages {
			if strings.Contains(m.Content, "<todo_reminder>") || strings.Contains(m.Content, "<todo_gate>") {
				t.Fatal("read-only run got a todo reminder")
			}
		}
	}
}

func TestTodoCompletionGateRespectsBreaker(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	p := &scripted{resps: []provider.Message{
		todoCall("c0", `{"items":[{"content":"write the tests","status":"pending"}]}`),
		{Role: provider.RoleAssistant, Content: "done"},
		pingCall("p1"),
		pingCall("p2"),
		pingCall("p3"),
		pingCall("p4"),
		{Role: provider.RoleAssistant, Content: "summarized"},
	}}
	k := &Kernel{
		Bot: b, Provider: p, MaxSteps: 10,
		Links: []link.Link{link.Terminal{}, pingLink{}},
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "finish", time.Unix(20, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) != 7 {
		t.Fatalf("calls %d, the gate or the breaker did not bound the turn", len(p.reqs))
	}
	gate := p.reqs[2].Messages[len(p.reqs[2].Messages)-1]
	if gate.Role != provider.RoleUser || !strings.Contains(gate.Content, "<todo_gate>") {
		t.Fatal("the unfinished list did not continue the turn once")
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var stopped bool
	for _, e := range entries {
		if strings.Contains(e.Message.Content, "<stop_reason>") {
			stopped = true
		}
	}
	if !stopped {
		t.Fatal("breaker did not hard-stop the turn")
	}
}

func TestTodoStaleListDoesNotGateUnrelatedQuestion(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	p := &scripted{}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 10, Links: []link.Link{link.Terminal{}, pingLink{}}}
	p.resps = append(p.resps,
		todoCall("t1", `{"items":[{"content":"refactor parser","status":"in_progress"},{"content":"write docs","status":"pending"}]}`),
		provider.Message{Role: provider.RoleAssistant, Content: "paused, waiting for you"},
		provider.Message{Role: provider.RoleAssistant, Content: "ok (gate reply turn1)"},
	)
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "refactor the parser", time.Unix(20, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	n1 := len(p.reqs)
	p.resps = append(p.resps,
		provider.Message{Role: provider.RoleAssistant, Content: "It is 10:00."},
		provider.Message{Role: provider.RoleAssistant, Content: "(reply to gate)"},
	)
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "what time is it?", time.Unix(4000, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if calls := len(p.reqs) - n1; calls != 1 {
		t.Fatalf("unrelated question made %d model calls", calls)
	}
	last := p.reqs[len(p.reqs)-1].Messages
	end := last[len(last)-1]
	if strings.Contains(end.Content, "<todo_gate>") || strings.Contains(end.Content, "<todo_reminder>") {
		t.Fatalf("old task pushed onto the new turn: %s", end.Content)
	}
	items, err := ReadTodos(b)
	if err != nil || len(items) != 0 {
		t.Fatalf("active list %+v %v", items, err)
	}
	raw, err := os.ReadFile(TodoArchivePath(b))
	if err != nil || !strings.Contains(string(raw), "refactor parser") {
		t.Fatalf("archive %s %v", raw, err)
	}
}

func TestTodoTimerWakeDoesNotGate(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	p := &scripted{}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 10, Links: []link.Link{link.Terminal{}, pingLink{}}}
	p.resps = append(p.resps,
		todoCall("t1", `{"items":[{"content":"old task","status":"pending"}]}`),
		provider.Message{Role: provider.RoleAssistant, Content: "later"},
		provider.Message{Role: provider.RoleAssistant, Content: "gate reply"},
	)
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "plan", time.Unix(20, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	n1 := len(p.reqs)
	p.resps = append(p.resps,
		provider.Message{Role: provider.RoleAssistant, Content: "drink water"},
		provider.Message{Role: provider.RoleAssistant, Content: "gate reply 2"},
	)
	ev := link.TerminalEvent("u1", "timer: drink water", time.Unix(9000, 0).UTC())
	ev.Source = link.NameTimer
	if err := k.Handle(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if calls := len(p.reqs) - n1; calls != 1 {
		t.Fatalf("timer wake made %d model calls", calls)
	}
	last := p.reqs[len(p.reqs)-1].Messages
	end := last[len(last)-1]
	if strings.Contains(end.Content, "<todo_gate>") || strings.Contains(end.Content, "<todo_reminder>") {
		t.Fatalf("timer wake carried the old task: %s", end.Content)
	}
	items, err := ReadTodos(b)
	if err != nil || len(items) != 1 || items[0].Content != "old task" {
		t.Fatalf("timer wake archived or dropped the list: %+v %v", items, err)
	}
}

func TestTodoGateIsUserMessage(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	p := &scripted{resps: []provider.Message{
		todoCall("t1", `{"items":[{"content":"a","status":"pending"},{"content":"b","status":"pending"},{"content":"c","status":"pending"}]}`),
		{Role: provider.RoleAssistant, Content: "all done"},
		{Role: provider.RoleAssistant, Content: "gate reply"},
	}}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 6, Links: []link.Link{link.Terminal{}, pingLink{}}}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "do abc", time.Unix(20, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	var saw bool
	for i, r := range p.reqs {
		last := r.Messages[len(r.Messages)-1]
		if !strings.Contains(last.Content, "<todo_gate>") {
			continue
		}
		saw = true
		if last.Role != provider.RoleUser {
			t.Fatalf("req %d ends on %s: %q", i, last.Role, last.Content)
		}
		if prev := r.Messages[len(r.Messages)-2]; prev.Role == provider.RoleAssistant && strings.Contains(prev.Content, "<todo_gate>") {
			t.Fatal("gate was also appended to the assistant message")
		}
	}
	if !saw {
		t.Fatal("gate did not run")
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Message.Content, "<todo_gate>") && e.Message.Role == provider.RoleAssistant {
			t.Fatalf("transcript attributes the gate to the model: %q", e.Message.Content)
		}
	}
}

func TestTodoQuestionAndApprovalSkipTheGate(t *testing.T) {
	b := testBot(t, link.NameTerminal, link.NameWeb)
	p := &scripted{resps: []provider.Message{
		todoCall("t1", `{"items":[{"content":"ship","status":"pending"}]}`),
		{Role: provider.RoleAssistant, Content: "Should I keep going?"},
		{Role: provider.RoleAssistant, Content: "should not run"},
	}}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 6, Links: []link.Link{link.Terminal{}, link.Web{}, pingLink{}}}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "ship it", time.Unix(20, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) != 2 {
		t.Fatalf("question continued: %d calls", len(p.reqs))
	}

	b2 := testBot(t, link.NameTerminal, link.NameWeb)
	card := bot.ApprovalCardText("ab12", "shell", "run", "", "mkdir x", bot.ApprovalPending)
	if err := b2.Append(bot.Entry{Time: time.Unix(1, 0).UTC(), Message: provider.Message{Role: provider.RoleUser, Content: card}}); err != nil {
		t.Fatal(err)
	}
	p2 := &scripted{resps: []provider.Message{
		todoCall("t1", `{"items":[{"content":"ship","status":"pending"}]}`),
		{Role: provider.RoleAssistant, Content: "done"},
		{Role: provider.RoleAssistant, Content: "should not run"},
	}}
	k2 := &Kernel{Bot: b2, Provider: p2, MaxSteps: 6, Links: []link.Link{link.Terminal{}, link.Web{}, pingLink{}}}
	if err := k2.Handle(context.Background(), link.TerminalEvent("u1", "ship it", time.Unix(30, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if len(p2.reqs) != 2 {
		t.Fatalf("approval card continued: %d calls", len(p2.reqs))
	}

	b3 := testBot(t, link.NameTerminal, link.NameWeb)
	p3 := &scripted{resps: []provider.Message{
		todoCall("t1", `{"items":[{"content":"ship","status":"pending"}]}`),
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "a1", Type: "function",
			Function: provider.FunctionCall{Name: "ask_user", Arguments: `{"question":"Ship it?","options":["yes","no"]}`},
		}}},
		{Role: provider.RoleAssistant, Content: "waiting for you"},
		{Role: provider.RoleAssistant, Content: "should not run"},
	}}
	k3 := &Kernel{Bot: b3, Provider: p3, MaxSteps: 6, Links: []link.Link{link.Terminal{}, link.Web{}, pingLink{}}}
	if err := k3.Handle(context.Background(), link.TerminalEvent("u1", "ship it", time.Unix(40, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if len(p3.reqs) != 3 {
		t.Fatalf("ask_user continued: %d calls", len(p3.reqs))
	}
}

func TestTodoOffDoesNotRewrite(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b}
	if _, err := (todoTool{k}).Run(context.Background(), json.RawMessage(`{"items":[{"content":"keep","status":"pending"}]}`)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(TodoPath(b))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetBinding("openai", "gpt-4"); err != nil {
		t.Fatal(err)
	}
	off := &Kernel{Bot: b, Links: []link.Link{link.Terminal{}, pingLink{}}}
	off.noteTodoStep([]provider.Message{{Role: provider.RoleTool, Name: "ping", Content: "pong"}})
	if text := off.todoNote(time.Unix(50, 0).UTC(), nil); text != "" {
		t.Fatalf("switch off reminded: %s", text)
	}
	raw2, err := os.ReadFile(TodoPath(b))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(raw2) {
		t.Fatalf("switch off rewrote the list\n%s\n%s", raw, raw2)
	}
}

func TestTodoStepCountStaysInMemory(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, TodoSteps: 2, Links: []link.Link{link.Terminal{}, pingLink{}}}
	if _, err := (todoTool{k}).Run(context.Background(), json.RawMessage(`{"items":[{"content":"write the tests","status":"pending"}]}`)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(TodoPath(b))
	if err != nil {
		t.Fatal(err)
	}
	ping := []provider.Message{{Role: provider.RoleTool, Name: "ping", Content: "pong"}}
	k.noteTodoStep(ping)
	k.noteTodoStep(ping)
	raw2, err := os.ReadFile(TodoPath(b))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(raw2) {
		t.Fatalf("counting steps rewrote todos.json\n%s\n%s", raw, raw2)
	}
	text := k.todoNote(time.Unix(60, 0).UTC(), nil)
	if !strings.Contains(text, "<todo_reminder>") || !strings.Contains(text, "write the tests") {
		t.Fatalf("in-memory count did not remind: %s", text)
	}
}

func TestTodoArchivesFinishedAndReplacedLists(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	k := &Kernel{Bot: b, Now: func() time.Time { return time.Unix(70, 0).UTC() }}
	tool := todoTool{k}
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"items":[{"id":"p","content":"parser","status":"in_progress"}]}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"items":[{"id":"p","content":"parser","status":"done"}]}`)); err != nil {
		t.Fatal(err)
	}
	items, err := ReadTodos(b)
	if err != nil || len(items) != 0 {
		t.Fatalf("finished list still active: %+v %v", items, err)
	}
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"items":[{"id":"n","content":"next","status":"pending"}]}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"items":[{"id":"z","content":"other","status":"pending"}]}`)); err != nil {
		t.Fatal(err)
	}
	items, err = ReadTodos(b)
	if err != nil || len(items) != 1 || items[0].Content != "other" {
		t.Fatalf("active %+v %v", items, err)
	}
	raw, err := os.ReadFile(TodoArchivePath(b))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "parser") || !strings.Contains(string(raw), "next") {
		t.Fatalf("archive missing replaced lists: %s", raw)
	}
	info, err := os.Stat(TodoArchivePath(b))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("archive mode %v %v", info, err)
	}
}

func BenchmarkTodoDiff(b *testing.B) {
	cur := make([]TodoItem, todoMaxItems)
	prev := make([]TodoItem, todoMaxItems)
	for i := range cur {
		cur[i] = TodoItem{ID: "t", Content: "item", Status: "pending"}
		prev[i] = cur[i]
		cur[i].ID = string(rune('a'+i%26)) + "id"
		prev[i].ID = cur[i].ID
	}
	cur[3].Status = "done"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if todoDiffText(cur, prev) == "" {
			b.Fatal("empty")
		}
	}
}
