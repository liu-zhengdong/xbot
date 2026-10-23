package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
	if msg.Text() != "Hello" || strings.Join(deltas, "") != "Hello" {
		t.Fatalf("content %q deltas %v", msg.Text(), deltas)
	}
	if len(msg.Calls()) != 1 {
		t.Fatalf("calls %+v", msg.Calls())
	}
	tc := msg.Calls()[0]
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
		if !strings.Contains(text, `"include_usage":true`) {
			t.Errorf("request missing include_usage: %s", text)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"pong\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "secret", Model: "mock", HTTPClient: srv.Client()}
	msg, err := c.Chat(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: TextContent("ping")}},
		Tools:    []ToolDef{{Name: "shell", Description: "run", Parameters: map[string]any{"type": "object"}}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Text() != "pong" || !sawAuth || !sawStream || !sawTool {
		t.Fatalf("msg=%+v auth=%v stream=%v tool=%v", msg, sawAuth, sawStream, sawTool)
	}
}

func TestChatMissingKey(t *testing.T) {
	c := &Client{BaseURL: "http://example", Model: "m"}
	if _, err := c.Chat(context.Background(), Request{}, nil); err == nil {
		t.Fatal("expected missing key")
	}
}

func TestChatRetriesEOFAndServerErrors(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		switch n {
		case 1:
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Error("no hijacker")
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			conn.Close()
		case 2:
			http.Error(w, "busy", http.StatusBadGateway)
		case 3:
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"pong\"}}]}\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
		default:
			http.Error(w, "extra", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "x", Model: "m", HTTPClient: srv.Client(), retryWait: time.Millisecond}
	var deltas []string
	msg, err := c.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: TextContent("hi")}}}, func(s string) {
		deltas = append(deltas, s)
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Text() != "pong" || strings.Join(deltas, "") != "pong" || n != 3 {
		t.Fatalf("msg %q deltas %v calls %d", msg.Text(), deltas, n)
	}
}

func TestChatDoesNotRetryClientErrors(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "x", Model: "m", HTTPClient: srv.Client(), retryWait: time.Millisecond}
	_, err := c.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: TextContent("hi")}}}, nil)
	if err == nil || !strings.Contains(err.Error(), "401") || n != 1 {
		t.Fatalf("err=%v calls=%d", err, n)
	}
}

func TestChatHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "x", Model: "m", HTTPClient: srv.Client()}
	_, err := c.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: TextContent("hi")}}}, nil)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatal(err)
	}
}

func TestDecodeStreamUsage(t *testing.T) {
	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"role":"assistant","content":"ok"}}]}`,
		`data: {"choices":[],"usage":{"prompt_tokens":800,"completion_tokens":12,"prompt_cache_hit_tokens":3,"prompt_tokens_details":{"cached_tokens":40}}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	msg, err := decodeStream(strings.NewReader(body), nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Text() != "ok" {
		t.Fatalf("content %q", msg.Text())
	}
	u := msg.Usage
	if !u.Reported || u.Estimated || u.PromptTokens != 800 || u.CompletionTokens != 12 || u.CachedTokens != 40 {
		t.Fatalf("usage %+v", u)
	}
}

func TestDecodeStreamUsageCacheHit(t *testing.T) {
	body := "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":1,\"prompt_cache_hit_tokens\":7}}\n\ndata: [DONE]\n"
	msg, err := decodeStream(strings.NewReader(body), nil)
	if err != nil {
		t.Fatal(err)
	}
	u := msg.Usage
	if !u.Reported || u.PromptTokens != 10 || u.CompletionTokens != 1 || u.CachedTokens != 7 {
		t.Fatalf("usage %+v", u)
	}
}

func TestDecodeStreamZeroUsageIsReported(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"z\"}}],\"usage\":{\"prompt_tokens\":0,\"completion_tokens\":0}}\n\ndata: [DONE]\n"
	msg, err := decodeStream(strings.NewReader(body), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !msg.Usage.Reported || msg.Usage.PromptTokens != 0 || msg.Usage.Estimated {
		t.Fatalf("usage %+v", msg.Usage)
	}
}

func TestDecodeStreamMissingUsageIsNotReported(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"z\"}}]}\n\ndata: [DONE]\n"
	msg, err := decodeStream(strings.NewReader(body), nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Usage.Reported || msg.Usage.Estimated || msg.Usage.PromptTokens != 0 {
		t.Fatalf("usage %+v", msg.Usage)
	}
}

// A cold cache reported as 0 and no cache figure at all both leave
// CachedTokens at 0; CacheReported tells them apart.
func TestDecodeStreamCacheFieldPresence(t *testing.T) {
	for _, tc := range []struct {
		usage string
		want  bool
	}{
		{`{"prompt_tokens":10,"completion_tokens":1}`, false},
		{`{"prompt_tokens":10,"completion_tokens":1,"prompt_cache_hit_tokens":0}`, true},
		{`{"prompt_tokens":10,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0}}`, true},
		{`{"prompt_tokens":10,"completion_tokens":1,"prompt_tokens_details":{}}`, false},
	} {
		body := "data: {\"choices\":[],\"usage\":" + tc.usage + "}\n\ndata: [DONE]\n"
		msg, err := decodeStream(strings.NewReader(body), nil)
		if err != nil {
			t.Fatal(err)
		}
		if msg.Usage.CacheReported != tc.want || msg.Usage.CachedTokens != 0 {
			t.Errorf("%s: cache reported %v, want %v", tc.usage, msg.Usage.CacheReported, tc.want)
		}
	}
}
