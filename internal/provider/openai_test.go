package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeStreamTextAndSplitToolCall(t *testing.T) {
	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"role":"assistant","content":"Hel"}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":"lo"}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"she","arguments":""}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"ll","arguments":"{\"command\":"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"ls\"}"}}]}}]}`,
		`data: [DONE]`,
		``,
	}, "\n")
	var deltas []string
	msg, err := decodeStream(strings.NewReader(body), func(s string) { deltas = append(deltas, s) })
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content != "Hello" || strings.Join(deltas, "") != "Hello" {
		t.Fatalf("content %q deltas %v", msg.Content, deltas)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("calls %+v", msg.ToolCalls)
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_1" || tc.Function.Name != "shell" || tc.Function.Arguments != `{"command":"ls"}` {
		t.Fatalf("%+v", tc)
	}
}

func TestDecodeStreamAPIError(t *testing.T) {
	body := `data: {"error":{"message":"bad key"}}` + "\n\n"
	_, err := decodeStream(strings.NewReader(body), nil)
	if err == nil || !strings.Contains(err.Error(), "bad key") {
		t.Fatal(err)
	}
}

func TestChatHTTP(t *testing.T) {
	var sawAuth, sawStream, sawTool bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") == "Bearer secret" {
			sawAuth = true
		}
		b, _ := io.ReadAll(r.Body)
		text := string(b)
		sawStream = strings.Contains(text, `"stream":true`)
		sawTool = strings.Contains(text, `"name":"shell"`)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"pong\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "secret", Model: "mock", HTTPClient: srv.Client()}
	msg, err := c.Chat(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "ping"}},
		Tools:    []ToolDef{{Name: "shell", Description: "run", Parameters: map[string]any{"type": "object"}}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content != "pong" || !sawAuth || !sawStream || !sawTool {
		t.Fatalf("msg=%+v auth=%v stream=%v tool=%v", msg, sawAuth, sawStream, sawTool)
	}
}

func TestChatMissingKey(t *testing.T) {
	c := &Client{BaseURL: "http://example", Model: "m"}
	if _, err := c.Chat(context.Background(), Request{}, nil); err == nil {
		t.Fatal("expected missing key")
	}
}

func TestChatHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "x", Model: "m", HTTPClient: srv.Client()}
	_, err := c.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}, nil)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatal(err)
	}
}
