package link

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
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
	need, summary, err := echo.Confirm(nil)
	if err != nil || need {
		t.Fatalf("readonly confirm %v %v", need, err)
	}
	need, summary, err = write.Confirm(nil)
	if err != nil || need || !strings.Contains(summary, "write") {
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

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
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
