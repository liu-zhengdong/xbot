package mcp

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
)

func TestMCPRoundTrip(t *testing.T) {
	var lists atomic.Int32
	m := NewSession(Spec{
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
	}, Options{})
	defer m.Close()
	if m.Name() != "mcp:docs" {
		t.Fatal(m.Name())
	}
	if err := m.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if first, second := awaitMCP(t, m.Notifications()), awaitMCP(t, m.Notifications()); first != "ping" || second != "pong" {
		t.Fatalf("notes=%q/%q", first, second)
	}
	list, err := m.LoadTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if again, err := m.LoadTools(context.Background()); err != nil || len(again) != len(list) || lists.Load() != 1 {
		t.Fatalf("lists %d tools %d/%d", lists.Load(), len(list), len(again))
	}
	byName := map[string]ToolSpec{}
	for _, tool := range list {
		byName[tool.Name] = tool
	}
	echo, write := byName["echo"], byName["write"]
	if echo.Name == "" || write.Name == "" || !echo.Annotations.ReadOnlyHint || write.Annotations.ReadOnlyHint {
		t.Fatalf("specs=%+v", list)
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(echo.InputSchema, &schema); err != nil || schema.Properties["q"] == nil {
		t.Fatal("schema lost", err)
	}
	out, err := m.CallTool(context.Background(), echo.Name, json.RawMessage(`{"q":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
			Data string `json:"data"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(out, &result); err != nil || len(result.Content) != 3 || result.Content[0].Type != "image" || result.Content[0].Data != "xx" || result.Content[1].Text != "hi" || result.Content[2].Text != "tail" {
		t.Fatalf("raw=%s error=%v", out, err)
	}
	out, err = m.CallTool(context.Background(), write.Name, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &result); err != nil || !result.IsError || result.Content[0].Text != "nope" {
		t.Fatalf("raw=%s error=%v", out, err)
	}
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
