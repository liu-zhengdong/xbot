package kernel

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

// registered says the tool is in the registry: callable through the door.
func registered(k *Kernel, name string) bool {
	_, ok := k.toolRegistry().find(name)
	return ok
}

// indexHas says the environment's tool_index names the tool.
func indexHas(e environment, name string) bool {
	for _, v := range environmentGet[map[string]any](e, "tool_index") {
		if ids, ok := v.([]any); ok {
			for _, id := range ids {
				if id == name {
					return true
				}
			}
		}
	}
	return false
}

func doorKernel(t *testing.T, p provider.Provider) (*Kernel, *bot.Bot) {
	t.Helper()
	b := testBot(t, link.NameTerminal, link.NameTools, link.NamePersona, link.NameMemory, link.NameTimer, link.NameBrowser)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 12, WorkDir: t.TempDir(),
		Links:   []link.Link{link.Terminal{}, link.NewToolsLink(), link.Persona{}, link.Memory{}, &link.Timer{Dir: b.RoutineDir(), Sender: "u1"}},
		Approve: func(context.Context, string, string) (bool, error) { return true, nil },
	}
	return k, b
}

// Every tool registers once with its source and group, on the native-or-Link
// cut: kernel and organs are ours, a store grant is a Link.
func TestRegistryListsEveryToolOnceWithSource(t *testing.T) {
	k, _ := doorKernel(t, nil)
	r := k.toolRegistry()
	seen := map[string]bool{}
	for _, e := range r.entries {
		if seen[e.tool.Name()] {
			t.Fatalf("%s registered twice", e.tool.Name())
		}
		seen[e.tool.Name()] = true
	}
	for name, source := range map[string]string{
		"read_file":            "organ",
		"send_message_to_user": "kernel",
		"bot_propose_persona":  "kernel",
		"desktop_click":        "organ",
		"browser_open":         "link",
	} {
		e, ok := r.find(name)
		if !ok || e.source != source {
			t.Fatalf("%s: %+v %v, want source %s", name, e, ok, source)
		}
	}

}

func TestRestartToolUsesKernelRegistryAndPassesHintToServiceHook(t *testing.T) {
	k, _ := doorKernel(t, nil)
	if _, ok := k.toolRegistry().find("restart_xbot"); ok {
		t.Fatal("restart tool registered without a service restart hook")
	}
	var got string
	k.Restart = func(hint string) error { got = hint; return nil }
	e, ok := k.toolRegistry().find("restart_xbot")
	if !ok || e.source != "kernel" || e.group != "system" {
		t.Fatalf("restart tool registry entry = %+v, %v", e, ok)
	}
	if _, err := e.tool.Run(context.Background(), json.RawMessage(`{"hint":"继续检查设备同步"}`)); err != nil {
		t.Fatal(err)
	}
	if got != "继续检查设备同步" {
		t.Fatalf("restart hook hint = %q", got)
	}
}

// tools[] is the door, send_message_to_user, the shells, files and
// web_fetch. Everything else is registered but undeclared.
func TestDeclaredToolsAreTheShortFixedSet(t *testing.T) {
	k, _ := doorKernel(t, nil)
	list, err := k.modelTools()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"edit_file", "read_file", "send_message_to_user", "tools", "web_fetch", "write_file"}
	for _, sh := range tools.NewShells() {
		want = append(want, sh.Name())
	}
	if k.todoToolOn() {
		want = append(want, "todo")
	}
	got := toolNames(declared(list))
	if strings.Join(got, ",") != strings.Join(toolNames(toolsNamed(want)), ",") {
		t.Fatalf("declared %v, want %v", got, want)
	}
	for _, name := range []string{"bot_propose_persona", "recall_history", "recall", "browser_open", "desktop_click", "links"} {
		if !hasToolName(list, name) || hasToolName(declared(list), name) {
			t.Fatalf("%s must be registered and undeclared", name)
		}
	}
}

func toolsNamed(names []string) []tools.Tool {
	var out []tools.Tool
	for _, n := range names {
		out = append(out, bindingSearchTool(n))
	}
	return out
}

func TestProfileModelGateRetiredPersonaRemainsUsable(t *testing.T) {
	k, b := doorKernel(t, nil)
	if registered(k, "bot_update_profile") {
		t.Fatal("retired profile tool remains callable")
	}
	for label, load := range map[string]func() ([]tools.Tool, error){"main": k.modelTools, "child": k.mountedTools} {
		list, err := load()
		if err != nil || hasToolName(list, "bot_update_profile") || !hasToolName(list, "bot_propose_persona") {
			t.Fatalf("%s tools still expose profile or lost persona: %v", label, err)
		}
	}
	door := toolDoor{k}
	ctx := context.Background()
	if _, err := door.Run(ctx, json.RawMessage(`{"action":"describe","id":"bot_update_profile"}`)); err == nil {
		t.Fatal("retired profile schema remains describable")
	}
	if tool, _, err := door.Forward(ctx, json.RawMessage(`{"action":"call","id":"bot_update_profile","args":{"description":"obsolete"}}`)); err == nil || tool != nil {
		t.Fatal("retired profile call still forwards")
	}
	tool, args, err := door.Forward(ctx, json.RawMessage(`{"action":"call","id":"bot_propose_persona","args":{"text":"Keep the remaining persona action working."}}`))
	if err != nil || tool == nil {
		t.Fatalf("persona forwarding: %v", err)
	}
	if _, err := tool.Run(ctx, args); err != nil {
		t.Fatal(err)
	}
	if text, err := b.ReadPersona(); err != nil || text != "Keep the remaining persona action working." {
		t.Fatalf("persona persistence: %q %v", text, err)
	}
}

func TestDoorSearchAndDescribe(t *testing.T) {
	k, _ := doorKernel(t, nil)
	door := toolDoor{k}
	ctx := context.Background()
	out, err := door.Run(ctx, json.RawMessage(`{"action":"search","query":"persona"}`))
	if err != nil || !strings.Contains(out, "bot_propose_persona [bot_profile]") {
		t.Fatalf("search %q %v", out, err)
	}
	if strings.Contains(out, `"properties"`) {
		t.Fatal("search dumped a schema")
	}
	out, err = door.Run(ctx, json.RawMessage(`{"action":"describe","id":"bot_propose_persona"}`))
	var desc struct {
		ID, Group, Source string
		Parameters        map[string]any
	}
	if err != nil || json.Unmarshal([]byte(out), &desc) != nil || desc.ID != "bot_propose_persona" || desc.Group != "bot_profile" || desc.Parameters["properties"] == nil {
		t.Fatalf("describe %q %v", out, err)
	}
	// Describing a native group lists its tools.
	out, err = door.Run(ctx, json.RawMessage(`{"action":"describe","id":"bot_profile"}`))
	if err != nil || !strings.Contains(out, `"bot_propose_persona"`) {
		t.Fatalf("describe group %q %v", out, err)
	}
	if _, err := door.Run(ctx, json.RawMessage(`{"action":"describe","id":"nope"}`)); err == nil || !strings.Contains(err.Error(), "search") {
		t.Fatalf("unknown id %v", err)
	}
	if indexHas(environment{}, "x") {
		t.Fatal("empty index")
	}
}

// The self-check of ADR 0035: several describes, a call through the door to
// a shell and native domain tools, and tools[] stays the
// same bytes in every request.
func TestDoorKeepsToolsBytesStableAndCalls(t *testing.T) {
	shell := tools.NewShells()[0].Name()
	p := &scripted{resps: []provider.Message{
		toolCall("s1", "tools", `{"action":"search","query":"persona"}`),
		toolCall("d1", "tools", `{"action":"describe","id":"bot_propose_persona"}`),
		toolCall("d2", "tools", `{"action":"describe","id":"bot_profile"}`),
		toolCall("c1", "tools", `{"action":"call","id":"`+shell+`","args":{"command":"echo door-shell","description":"echo"}}`),
		toolCall("c2", "tools", `{"action":"call","id":"recall_history","args":"{\"query\":\"DOOR_HISTORY_52\"}"}`),
		toolCall("c3", "tools", `{"action":"call","id":"recall_history","args":{"query":"DOOR_HISTORY_52"}}`),
		toolCall("bad", "tools", `{"action":"call","id":"recall_history","args":{"query":3}}`),
		{Role: provider.RoleAssistant, Content: provider.TextContent("done")},
	}}
	k, b := doorKernel(t, p)
	if err := b.Append(bot.Entry{Origin: bot.OriginOwner, Time: time.Unix(0, 0).UTC(), Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("DOOR_HISTORY_52 persisted native history fixture")}}); err != nil {
		t.Fatal(err)
	}
	var approved []string
	k.Approve = func(_ context.Context, name, _ string) (bool, error) {
		approved = append(approved, name)
		return true, nil
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "go", time.Unix(1, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if len(p.reqs) != len(p.resps) {
		t.Fatalf("requests %d", len(p.reqs))
	}
	first, _ := json.Marshal(p.reqs[0].Tools)
	for i, req := range p.reqs {
		raw, _ := json.Marshal(req.Tools)
		if string(raw) != string(first) {
			t.Fatalf("request %d changed tools[]: %v", i, toolNamesOf(req.Tools))
		}
	}
	text := transcriptText(t, b)
	for _, want := range []string{"bot_propose_persona [bot_profile]", `"text"`, "recall_history", "door-shell", "DOOR_HISTORY_52 persisted native history fixture"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in transcript:\n%s", want, text)
		}
	}
	results := map[string]string{}
	for _, message := range p.reqs[len(p.reqs)-1].Messages {
		if message.Role == provider.RoleTool {
			results[message.CallID()] = message.Text()
		}
	}
	for _, id := range []string{"c2", "c3"} {
		if !strings.Contains(results[id], "DOOR_HISTORY_52 persisted native history fixture") {
			t.Fatalf("native history forwarding %s: %q", id, results[id])
		}
	}
	if !strings.Contains(results["bad"], "cannot unmarshal number") {
		t.Fatalf("native tool error was not forwarded: %q", results["bad"])
	}
	for _, name := range approved {
		if name == "tools" {
			t.Fatal("a card named the door instead of the tool")
		}
	}
	// A later turn reaches the native tool by id alone, then by an ordinary
	// undeclared call; neither path needs schema promotion or a new load.
	p2 := &scripted{resps: []provider.Message{
		toolCall("c4", "tools", `{"action":"call","id":"recall_history","args":{"query":"DOOR_HISTORY_52"}}`),
		toolCall("c5", "recall_history", `{"query":"DOOR_HISTORY_52"}`),
		{Role: provider.RoleAssistant, Content: provider.TextContent("again")},
	}}
	k.Provider = p2
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "again", time.Unix(2, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	for id, name := range map[string]string{"c4": "tools", "c5": "recall_history"} {
		var history string
		for _, msg := range p2.reqs[len(p2.reqs)-1].Messages {
			if msg.Role == provider.RoleTool && msg.CallID() == id && msg.Name == name {
				history = msg.Text()
			}
		}
		if !strings.Contains(history, "DOOR_HISTORY_52 persisted native history fixture") {
			t.Fatalf("second turn native history %s: %q", id, history)
		}
	}
	published := testPublished(t, k)
	if !indexHas(published, "bot_propose_persona") {
		t.Fatal("tool_index lacks a listed tool")
	}
	for _, name := range []string{"create_bot", "delete_bot", "list_bots", "bot_status", "routine_create", "routine_list", "routine_update", "routine_delete"} {
		if indexHas(published, name) || registered(k, name) {
			t.Fatalf("retired tool %s remains available", name)
		}
	}
}
