package link

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"xbot/internal/tools"
)

func TestMCPRoundTrip(t *testing.T) {
	var lists atomic.Int32
	m := NewMCP(MCPServer{
		Name:    "docs",
		Command: "fake",
		Dial: dialFake(t, func(msg rpcMessage) []rpcMessage {
			switch msg.Method {
			case "notifications/initialized":
				return []rpcMessage{
					rpcNote("notifications/xbot/event", `{"text":"  "}`),
					rpcNote("notifications/xbot/event", `{"text":"ping"}`),
					rpcNote("notifications/message", `{"text":"pong"}`),
				}
			case "initialize":
				return []rpcMessage{rpcOK(msg.ID, `{"protocolVersion":"2024-11-05","capabilities":{}}`)}
			case "tools/list":
				lists.Add(1)
				return []rpcMessage{rpcOK(msg.ID, `{
					"tools":[
						{"name":"echo","description":"Echo","inputSchema":{"type":"object","properties":{"q":{"type":"string"}},"additionalProperties":false},"annotations":{"readOnlyHint":true}},
						{"name":"write","description":"Write","inputSchema":{"type":"object"},"annotations":{"readOnlyHint":false}}
					]
				}`)}
			case "tools/call":
				return []rpcMessage{rpcOK(msg.ID, callReply(msg.Params))}
			default:
				return nil
			}
		}),
	})
	if m.Name() != "mcp:docs" {
		t.Fatal(m.Name())
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan Event, 4)
	done := make(chan error, 1)
	go func() {
		done <- m.Run(ctx, func(ev Event) error {
			got <- ev
			return nil
		})
	}()
	var events []Event
	deadline := time.After(3 * time.Second)
	for len(events) < 2 {
		select {
		case ev := <-got:
			events = append(events, ev)
		case <-deadline:
			t.Fatalf("events %d", len(events))
		}
	}
	if events[0].Content != "ping" || events[1].Content != "pong" {
		t.Fatalf("%q %q", events[0].Content, events[1].Content)
	}
	for _, ev := range events {
		if ev.Source != "mcp:docs" || ev.Sender != "mcp:docs" || ev.Type != TypeMessage || !ev.Untrusted {
			t.Fatalf("%+v", ev)
		}
	}
	list := m.Tools()
	if again := m.Tools(); len(again) != len(list) || lists.Load() != 1 {
		t.Fatalf("lists %d tools %d/%d", lists.Load(), len(list), len(again))
	}
	byName := map[string]tools.Tool{}
	for _, tool := range list {
		byName[tool.Name()] = tool
	}
	echo := byName["mcp_docs_echo"]
	write := byName["mcp_docs_write"]
	if echo == nil || write == nil {
		t.Fatalf("%v", byName)
	}
	schema, ok := echo.Parameters().(map[string]any)
	if !ok {
		t.Fatalf("%T", echo.Parameters())
	}
	props, _ := schema["properties"].(map[string]any)
	if props["q"] == nil {
		t.Fatalf("%v", schema)
	}
	need, _, err := echo.Confirm(nil)
	if err != nil || need {
		t.Fatalf("readonly confirm %v %v", need, err)
	}
	need, summary, err := write.Confirm(nil)
	if err != nil || !need || !strings.Contains(summary, "write") {
		t.Fatalf("write confirm %v %q %v", need, summary, err)
	}
	out, err := echo.Run(context.Background(), []byte(`{"q":"hi"}`))
	if err != nil || out != "hi\ntail" {
		t.Fatalf("%q %v", out, err)
	}
	if _, err := write.Run(context.Background(), []byte(`{}`)); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("run did not stop")
	}
}

func TestMCPNotesDoNotBlockTools(t *testing.T) {
	m := NewMCP(MCPServer{
		Name:    "docs",
		Command: "fake",
		Dial: dialFake(t, func(msg rpcMessage) []rpcMessage {
			switch msg.Method {
			case "notifications/initialized":
				return nil
			case "initialize":
				return []rpcMessage{rpcOK(msg.ID, `{"protocolVersion":"2024-11-05"}`)}
			case "tools/list":
				out := make([]rpcMessage, 0, 41)
				for i := 0; i < 40; i++ {
					out = append(out, rpcNote("notifications/xbot/event", `{"text":"n"}`))
				}
				out = append(out, rpcOK(msg.ID, `{"tools":[{"name":"echo","inputSchema":{"type":"object"}}]}`))
				return out
			default:
				return nil
			}
		}),
	})
	ch := make(chan []tools.Tool, 1)
	go func() { ch <- m.Tools() }()
	select {
	case list := <-ch:
		if len(list) != 1 || list[0].Name() != "mcp_docs_echo" {
			t.Fatalf("%d", len(list))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("tools/list blocked on notifications")
	}
}

func TestMCPToolListStopsAtPageCap(t *testing.T) {
	var n atomic.Int32
	m := NewMCP(MCPServer{
		Name:    "docs",
		Command: "fake",
		Dial: dialFake(t, func(msg rpcMessage) []rpcMessage {
			switch msg.Method {
			case "notifications/initialized":
				return nil
			case "initialize":
				return []rpcMessage{rpcOK(msg.ID, `{"protocolVersion":"2024-11-05"}`)}
			case "tools/list":
				i := n.Add(1)
				body := fmt.Sprintf(`{"tools":[{"name":"t%d","inputSchema":{"type":"object"}}],"nextCursor":"c%d"}`, i, i)
				return []rpcMessage{rpcOK(msg.ID, body)}
			default:
				return nil
			}
		}),
	})
	list := m.Tools()
	if n.Load() != mcpPageCap || len(list) != mcpPageCap {
		t.Fatalf("pages %d tools %d", n.Load(), len(list))
	}
}

func TestMCPSecretsStayNames(t *testing.T) {
	t.Setenv("MCP_OK", "present-value")
	t.Setenv("MCP_SHOULD_NOT_LEAK", "leak-value")
	env := spawnEnv([]string{"MCP_OK"}, os.LookupEnv)
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "MCP_OK=present-value") || strings.Contains(joined, "leak-value") || strings.Contains(joined, "MCP_SHOULD_NOT_LEAK") {
		t.Fatal(joined)
	}
	if _, ok := os.LookupEnv("PATH"); ok && !strings.Contains(joined, "PATH=") {
		t.Fatal(joined)
	}
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := AddMCP(path, MCPServer{Name: "docs", Command: "/bin/echo", SecretEnvs: []string{"MCP_OK"}}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("present-value")) || bytes.Contains(raw, []byte("leak-value")) || !bytes.Contains(raw, []byte("MCP_OK")) {
		t.Fatalf("file %s", raw)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("mode %v %v", fi, err)
	}
	if err := AddMCP(path, MCPServer{Name: "docs", Command: "/bin/echo"}); !errors.Is(err, ErrMCPExists) {
		t.Fatal(err)
	}
	if err := AddMCP(path, MCPServer{Name: "bad", Command: "echo\nrm"}); err == nil {
		t.Fatal("newline command accepted")
	}
	if err := AddMCP(path, MCPServer{Name: "bad", Command: "/bin/echo", SecretEnvs: []string{"token"}}); err == nil {
		t.Fatal("lowercase secret accepted")
	}
	t.Setenv("XBOT_API_KEY", "sk-provider")
	SetProviderKey(func(name string) bool { return name == "XBOT_API_KEY" })
	defer SetProviderKey(nil)
	leaked := spawnEnv([]string{"XBOT_API_KEY", "MCP_OK"}, func(k string) (string, bool) {
		if k == "XBOT_API_KEY" {
			return "sk-provider", true
		}
		return os.LookupEnv(k)
	})
	if strings.Contains(strings.Join(leaked, "\n"), "sk-provider") || strings.Contains(strings.Join(leaked, "\n"), "XBOT_API_KEY=") {
		t.Fatalf("provider key passed: %s", leaked)
	}
	if err := AddMCP(path, MCPServer{Name: "bad", Command: "/bin/echo", SecretEnvs: []string{"XBOT_API_KEY"}}); err == nil {
		t.Fatal("provider key accepted")
	}
	raw, _ = os.ReadFile(path)
	if bytes.Contains(raw, []byte("token")) || bytes.Contains(raw, []byte("echo\n")) {
		t.Fatalf("rejected record stored: %s", raw)
	}
	if err := RemoveMCP(path, "missing"); !errors.Is(err, ErrMCPMissing) {
		t.Fatal(err)
	}
	if err := RemoveMCP(path, "docs"); err != nil {
		t.Fatal(err)
	}
	list, err := LoadMCP(path)
	if err != nil || len(list) != 0 {
		t.Fatalf("%v %v", list, err)
	}
	tokenPath := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(tokenPath, []byte(`{"servers":[{"name":"docs","command":"/bin/echo","token":"sek"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMCP(tokenPath); err == nil {
		t.Fatal("token field accepted")
	}
}

func TestLocalToolNameFits(t *testing.T) {
	used := map[string]bool{}
	name := localToolName("docs", strings.Repeat("a", 80), used)
	if len(name) > 64 || !used[name] {
		t.Fatal(name, len(name))
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			t.Fatalf("char %q in %s", r, name)
		}
	}
	again := localToolName("docs", strings.Repeat("a", 80), used)
	if again == name || len(again) > 64 {
		t.Fatal(again)
	}
	if Blurb("mcp:docs") == "mcp:docs" || !strings.Contains(Blurb("mcp:docs"), "untrusted") {
		t.Fatal(Blurb("mcp:docs"))
	}
}

func dialFake(t *testing.T, on func(rpcMessage) []rpcMessage) func(context.Context) (io.ReadWriteCloser, error) {
	t.Helper()
	client, server := net.Pipe()
	go func() {
		defer server.Close()
		sc := bufio.NewScanner(server)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			var msg rpcMessage
			if err := json.Unmarshal(bytes.TrimSpace(sc.Bytes()), &msg); err != nil {
				return
			}
			for _, out := range on(msg) {
				raw, err := json.Marshal(out)
				if err != nil {
					return
				}
				if _, err := server.Write(append(raw, '\n')); err != nil {
					return
				}
			}
		}
	}()
	t.Cleanup(func() { _ = client.Close() })
	return func(context.Context) (io.ReadWriteCloser, error) { return client, nil }
}

func rpcOK(id json.RawMessage, result string) rpcMessage {
	return rpcMessage{JSONRPC: "2.0", ID: append(json.RawMessage(nil), id...), Result: json.RawMessage(result)}
}

func rpcNote(method, params string) rpcMessage {
	return rpcMessage{JSONRPC: "2.0", Method: method, Params: json.RawMessage(params)}
}

func callReply(params json.RawMessage) string {
	var in struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	_ = json.Unmarshal(params, &in)
	if in.Name == "write" {
		return `{"content":[{"type":"text","text":"nope"}],"isError":true}`
	}
	q, _ := in.Arguments["q"].(string)
	return `{"content":[{"type":"image","data":"xx"},{"type":"text","text":"` + q + `"},{"type":"text","text":"tail"}]}`
}
