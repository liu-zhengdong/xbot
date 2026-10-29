package kernel

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
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

// pingMCP is an MCP server with one tool, ping, that answers pong.
func pingMCP(t *testing.T) func(context.Context) (io.ReadWriteCloser, error) {
	t.Helper()
	return func(context.Context) (io.ReadWriteCloser, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			sc := bufio.NewScanner(server)
			for sc.Scan() {
				var msg struct {
					ID     json.RawMessage `json:"id"`
					Method string          `json:"method"`
				}
				if json.Unmarshal(sc.Bytes(), &msg) != nil || len(msg.ID) == 0 {
					continue
				}
				result := `{}`
				switch msg.Method {
				case "initialize":
					result = `{"protocolVersion":"2024-11-05","capabilities":{}}`
				case "tools/list":
					result = `{"tools":[{"name":"ping","description":"Answer pong.","inputSchema":{"type":"object","properties":{"say":{"type":"string"}}},"annotations":{"readOnlyHint":true}}]}`
				case "tools/call":
					result = `{"content":[{"type":"text","text":"pong"}]}`
				}
				_, _ = server.Write([]byte(`{"jsonrpc":"2.0","id":` + string(msg.ID) + `,"result":` + result + "}\n"))
			}
		}()
		t.Cleanup(func() { _ = client.Close() })
		return client, nil
	}
}

func doorKernel(t *testing.T, p provider.Provider) (*Kernel, *bot.Bot) {
	t.Helper()
	b := testBot(t, link.NameTerminal, link.NameTools, link.NamePersona, link.NameMemory, link.NameTimer, link.NameBrowser)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	if err := link.AddMCP(b.GlobalMCPPath(), link.MCPServer{Name: "ping", Command: "fake"}); err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 12, WorkDir: t.TempDir(),
		Links:   []link.Link{link.Terminal{}, link.NewToolsLink(), link.Persona{}, link.Memory{}, &link.Timer{Dir: b.RoutineDir(), Sender: "u1"}},
		Approve: func(context.Context, string, string) (bool, error) { return true, nil },
	}
	k.newMCP = func(s link.MCPServer) link.Link {
		s.Dial = pingMCP(t)
		m := link.NewMCP(s)
		t.Cleanup(func() { _ = m.Close() })
		return m
	}
	return k, b
}

// Every tool registers once with its source and group, on the native-or-Link
// cut: kernel and organs are ours, a store grant is a Link, MCP is mcp:<name>.
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
		"read_file":            "kernel",
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
	if len(r.pending) != 1 || r.pending[0].id != "mcp:ping" {
		t.Fatalf("pending %+v", r.pending)
	}
	if err := k.startGroup(context.Background(), "mcp:ping"); err != nil {
		t.Fatal(err)
	}
	if e, ok := k.toolRegistry().find("mcp_ping_ping"); !ok || e.source != "mcp:ping" || e.group != "mcp:ping" || e.expose != exposeListed {
		t.Fatalf("mcp entry %+v %v", e, ok)
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
	out, err = door.Run(ctx, json.RawMessage(`{"action":"search","query":"ping"}`))
	if err != nil || !strings.Contains(out, "mcp:ping [group, not started]") {
		t.Fatalf("search pending %q %v", out, err)
	}
	out, err = door.Run(ctx, json.RawMessage(`{"action":"describe","id":"bot_update_profile"}`))
	var desc struct {
		ID, Group, Source string
		Parameters        map[string]any
	}
	if err != nil || json.Unmarshal([]byte(out), &desc) != nil || desc.ID != "bot_update_profile" || desc.Group != "bot_profile" || desc.Parameters["properties"] == nil {
		t.Fatalf("describe %q %v", out, err)
	}
	// Describing a group starts its server and lists its tools.
	out, err = door.Run(ctx, json.RawMessage(`{"action":"describe","id":"ping"}`))
	if err != nil || !strings.Contains(out, `"mcp_ping_ping"`) {
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
// a shell and to an MCP server never started before, and tools[] stays the
// same bytes in every request.
func TestDoorKeepsToolsBytesStableAndCalls(t *testing.T) {
	shell := tools.NewShells()[0].Name()
	p := &scripted{resps: []provider.Message{
		toolCall("s1", "tools", `{"action":"search","query":"routine"}`),
		toolCall("d1", "tools", `{"action":"describe","id":"routine_create"}`),
		toolCall("d2", "tools", `{"action":"describe","id":"mcp:ping"}`),
		toolCall("c1", "tools", `{"action":"call","id":"`+shell+`","args":{"command":"echo door-shell","description":"echo"}}`),
		toolCall("c2", "tools", `{"action":"call","id":"mcp_ping_ping","args":"{\"say\":\"hi\"}"}`),
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
	for _, want := range []string{"routine_create [routine]", `"precheck"`, "mcp_ping_ping", "door-shell", "pong", "no routines"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in transcript:\n%s", want, text)
		}
	}
	for _, name := range approved {
		if name == "tools" {
			t.Fatal("a card named the door instead of the tool")
		}
	}
	// A later turn reaches the server again by tool id alone.
	p2 := &scripted{resps: []provider.Message{
		toolCall("c4", "tools", `{"action":"call","id":"mcp_ping_ping","args":{}}`),
		{Role: provider.RoleAssistant, Content: provider.TextContent("again")},
	}}
	k.Provider = p2
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "again", time.Unix(2, 0).UTC())); err != nil {
		t.Fatal(err)
	}
	if got := messagesText(p2.reqs[1].Messages); !strings.Contains(got, "pong") {
		t.Fatalf("second turn call %q", got)
	}
	if !indexHas(testPublished(t, k), "routine_create") {
		t.Fatal("tool_index lacks a listed tool")
	}
}
