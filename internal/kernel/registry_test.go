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
		"bot_update_profile":   "kernel",
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
	for _, name := range []string{"bot_update_profile", "routine_create", "recall", "browser_open", "desktop_click", "links"} {
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

func TestDoorSearchAndDescribe(t *testing.T) {
	k, _ := doorKernel(t, nil)
	door := toolDoor{k}
	ctx := context.Background()
	out, err := door.Run(ctx, json.RawMessage(`{"action":"search","query":"avatar profile"}`))
	if err != nil || !strings.Contains(out, "bot_update_profile [bot_profile]") {
		t.Fatalf("search %q %v", out, err)
	}
	if strings.Contains(out, `"properties"`) {
		t.Fatal("search dumped a schema")
	}
	out, err = door.Run(ctx, json.RawMessage(`{"action":"describe","id":"bot_update_profile"}`))
	var desc struct {
		ID, Group, Source string
		Parameters        map[string]any
	}
	if err != nil || json.Unmarshal([]byte(out), &desc) != nil || desc.ID != "bot_update_profile" || desc.Group != "bot_profile" || desc.Parameters["properties"] == nil {
		t.Fatalf("describe %q %v", out, err)
	}
	// Describing a native group lists its tools.
	out, err = door.Run(ctx, json.RawMessage(`{"action":"describe","id":"bot_profile"}`))
	if err != nil || !strings.Contains(out, `"bot_update_profile"`) {
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
		toolCall("s1", "tools", `{"action":"search","query":"routine"}`),
		toolCall("d1", "tools", `{"action":"describe","id":"routine_create"}`),
		toolCall("d2", "tools", `{"action":"describe","id":"routine"}`),
		toolCall("c1", "tools", `{"action":"call","id":"`+shell+`","args":{"command":"echo door-shell","description":"echo"}}`),
		toolCall("c2", "tools", `{"action":"call","id":"bot_status","args":"{}"}`),
		toolCall("c3", "tools", `{"action":"call","id":"routine_list","args":{}}`),
		{Role: provider.RoleAssistant, Content: provider.TextContent("done")},
	}}
	k, b := doorKernel(t, p)
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
	for _, want := range []string{"routine_create [routine]", `"precheck"`, "bot_status", "door-shell", "no routines"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in transcript:\n%s", want, text)
		}
	}
	for _, name := range approved {
		if name == "tools" {
			t.Fatal("a card named the door instead of the tool")
		}
	}
	// A later turn reaches the native tool again by id alone.
	p2 := &scripted{resps: []provider.Message{
		toolCall("c4", "tools", `{"action":"call","id":"routine_list","args":{}}`),
		{Role: provider.RoleAssistant, Content: provider.TextContent("again")},
	}}
	k.Provider = p2
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "again", time.Unix(2, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if got := messagesText(p2.reqs[1].Messages); !strings.Contains(got, "no routines") {
		t.Fatalf("second turn call %q", got)
	}
	if !indexHas(testPublished(t, k), "routine_create") {
		t.Fatal("tool_index lacks a listed tool")
	}
}
