package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// requestThinking sends one chat through c and returns the request's thinking
// field, or nil when the field was left out.
func requestThinking(t *testing.T, c *Client) map[string]any {
	t.Helper()
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &body); err != nil {
			t.Errorf("request body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	c.BaseURL, c.APIKey, c.HTTPClient = srv.URL, "k", srv.Client()
	if _, err := c.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: TextContent("hi")}}}, nil); err != nil {
		t.Fatal(err)
	}
	th, ok := body["thinking"]
	if !ok {
		return nil
	}
	m, _ := th.(map[string]any)
	if m == nil {
		t.Fatalf("thinking is not an object: %#v", th)
	}
	return m
}

// DeepSeek models think by default, and XBot now replays reasoning_content on
// tool turns, so every DeepSeek request asks for thinking explicitly.
func TestDeepSeekRequestEnablesThinking(t *testing.T) {
	for _, model := range []string{"deepseek-flash", "deepseek-chat", "deepseek-v4-pro", "group/auto-deepseek-v4-1-flash"} {
		got := requestThinking(t, &Client{Model: model})
		if got == nil || got["type"] != "enabled" {
			t.Fatalf("%s: thinking = %#v, want type enabled", model, got)
		}
	}
}

func TestThinkingSettingOverridesModelName(t *testing.T) {
	if got := requestThinking(t, &Client{Model: "deepseek-flash", Thinking: ThinkingOmit}); got != nil {
		t.Fatalf("omit still sent %#v", got)
	}
	if got := requestThinking(t, &Client{Model: "other-model", Thinking: ThinkingOff}); got == nil || got["type"] != "disabled" {
		t.Fatalf("off on a non-DeepSeek model sent %#v", got)
	}
	if got := requestThinking(t, &Client{Model: "deepseek-flash", Thinking: ThinkingOff}); got == nil || got["type"] != "disabled" {
		t.Fatalf("off on DeepSeek sent %#v", got)
	}
	if got := requestThinking(t, &Client{Model: "other-model", Thinking: ThinkingOn}); got == nil || got["type"] != "enabled" {
		t.Fatalf("on on a non-DeepSeek model sent %#v", got)
	}
}

func TestNonDeepSeekAndReasonerSendNoThinking(t *testing.T) {
	for _, model := range []string{"mock", "gpt-4o", "deepseek-reasoner"} {
		if got := requestThinking(t, &Client{Model: model}); got != nil {
			t.Fatalf("%s: unexpected thinking %#v", model, got)
		}
	}
}

func TestReasoningIsKeptButNotStreamed(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"think \"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"hard\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"answer\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"function\":{\"name\":\"ls\",\"arguments\":\"{}\"}}]}}]}\n\n" +
		"data: [DONE]\n\n"
	var live strings.Builder
	msg, err := decodeStream(strings.NewReader(stream), func(s string) { live.WriteString(s) })
	if err != nil {
		t.Fatal(err)
	}
	if live.String() != "answer" || msg.Text() != "answer" {
		t.Fatalf("live %q text %q", live.String(), msg.Text())
	}
	if calls := msg.Calls(); len(calls) != 1 || calls[0].ID != "c1" || calls[0].Function.Name != "ls" {
		t.Fatalf("calls %+v", msg.Calls())
	}
	if got, ok := replayReasoning(msg); !ok || got != "think hard" {
		t.Fatalf("reasoning %q %v", got, ok)
	}
	// It survives the transcript round trip.
	raw, _ := json.Marshal(msg)
	var back Message
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if got, _ := replayReasoning(back); got != "think hard" || back.Text() != "answer" {
		t.Fatalf("after round trip %q %q", got, back.Text())
	}
}

func TestWireReplaysReasoningOnlyWhileThinking(t *testing.T) {
	own := Message{Role: RoleAssistant, Content: append([]ContentPart{reasoningPart("why")}, ToolContent([]ToolCall{{ID: "c1", Type: "function", Function: FunctionCall{Name: "ls", Arguments: "{}"}}})...)}
	legacy := Message{Role: RoleAssistant, Content: ToolContent([]ToolCall{{ID: "c2", Type: "function", Function: FunctionCall{Name: "ls", Arguments: "{}"}}})}
	plain := Message{Role: RoleAssistant, Content: TextContent("hi")}
	user := Message{Role: RoleUser, Content: TextContent("q")}
	on, err := wireMessages([]Message{user, own, legacy, plain}, true)
	if err != nil {
		t.Fatal(err)
	}
	if on[0].ReasoningContent != nil || on[3].ReasoningContent != nil {
		t.Fatalf("reasoning on a user or plain message: %+v", on)
	}
	if on[1].ReasoningContent == nil || *on[1].ReasoningContent != "why" || len(on[1].Content) != 0 {
		t.Fatalf("own reasoning %+v content %s", on[1].ReasoningContent, on[1].Content)
	}
	if on[2].ReasoningContent == nil || *on[2].ReasoningContent != "" {
		t.Fatalf("legacy tool turn needs an empty reasoning_content: %+v", on[2].ReasoningContent)
	}
	off, err := wireMessages([]Message{user, own, legacy, plain}, false)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(off)
	if strings.Contains(string(raw), "reasoning") || strings.Contains(string(raw), "why") {
		t.Fatalf("reasoning sent with thinking off: %s", raw)
	}
}

// The second tool turn of a thinking conversation carries the first turn's
// reasoning_content; DeepSeek returns 400 without it.
func TestThinkingMultiTurnToolReplay(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "text/event-stream")
		n := len(bodies)
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"r%d\"}}]}\n\n", n)
		if n < 3 {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c%d\",\"function\":{\"name\":\"ls\",\"arguments\":\"{}\"}}]}}]}\n\n", n)
		} else {
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"}}]}\n\n")
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", Model: "deepseek-flash", Thinking: ThinkingOn, HTTPClient: srv.Client()}
	msgs := []Message{{Role: RoleUser, Content: TextContent("go")}}
	for i := 0; i < 3; i++ {
		out, err := c.Chat(context.Background(), Request{Messages: msgs, Tools: []ToolDef{{Name: "ls", Parameters: map[string]any{"type": "object"}}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		msgs = append(msgs, out)
		for _, call := range out.Calls() {
			msgs = append(msgs, Message{Role: RoleTool, Content: ToolResult(call.ID, "ok")})
		}
	}
	if msgs[len(msgs)-1].Text() != "done" {
		t.Fatalf("final %+v", msgs[len(msgs)-1])
	}
	last := bodies[2]["messages"].([]any)
	var got []string
	for _, m := range last {
		m := m.(map[string]any)
		if m["role"] == "assistant" {
			got = append(got, fmt.Sprint(m["reasoning_content"]))
			if strings.Contains(fmt.Sprint(m["content"]), "thinking") {
				t.Fatalf("reasoning marker in content: %v", m)
			}
		}
	}
	if strings.Join(got, ",") != "r1,r2" {
		t.Fatalf("replayed reasoning %v", got)
	}
}

func TestAnthropicThinkingOnRequestsBudget(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		http.Error(w, "stop", http.StatusBadRequest)
	}))
	defer srv.Close()
	p, err := New(Endpoint{Protocol: AnthropicMessages, BaseURL: srv.URL, APIKey: "k", Model: "claude", Thinking: ThinkingOn, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = p.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: TextContent("hi")}}}, nil)
	th, _ := body["thinking"].(map[string]any)
	if th["type"] != "enabled" || th["budget_tokens"].(float64) >= body["max_tokens"].(float64) {
		t.Fatalf("thinking %v max %v", th, body["max_tokens"])
	}
}

func TestReasoningBufferLimit(t *testing.T) {
	chunk := `data: {"choices":[{"delta":{"reasoning_content":"` + strings.Repeat("x", 1<<15) + `"}}]}` + "\n\n"
	for _, n := range []int{32, 33} {
		_, err := decodeStream(strings.NewReader(strings.Repeat(chunk, n)+"data: [DONE]\n\n"), nil)
		if (err != nil) != (n == 33) {
			t.Fatalf("%d chunks: %v", n, err)
		}
	}
}

func TestThinkingEffortAndOmittedSwitchReplay(t *testing.T) {
	for _, setting := range []string{"", ThinkingOn, ThinkingOmit, ThinkingOff} {
		for _, effort := range []string{"", "low", "high", "max"} {
			t.Run(setting+"/"+effort, func(t *testing.T) {
				var body map[string]any
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
				}))
				defer srv.Close()
				c := &Client{BaseURL: srv.URL, APIKey: "fake", Model: "deepseek-flash", Thinking: setting, ReasoningEffort: effort}
				own := Message{Role: RoleAssistant, Content: []ContentPart{reasoningPart("private")}}
				_, err := c.Chat(context.Background(), Request{Messages: []Message{own}}, nil)
				if err != nil {
					t.Fatal(err)
				}
				msg := body["messages"].([]any)[0].(map[string]any)
				if setting == ThinkingOff {
					if body["reasoning_effort"] != nil || msg["reasoning_content"] != nil {
						t.Fatalf("off: %v", body)
					}
				} else {
					want := effort
					if want == "" {
						want = "low"
					}
					if body["reasoning_effort"] != want || msg["reasoning_content"] != "private" {
						t.Fatalf("thinking: %v", body)
					}
				}
				if setting == ThinkingOmit && body["thinking"] != nil {
					t.Fatalf("omit: %v", body)
				}
			})
		}
	}
}

func TestResponsesThinkingUsesNativeEffort(t *testing.T) {
	for _, setting := range []string{"", ThinkingOn, ThinkingOff, ThinkingOmit} {
		t.Run(setting, func(t *testing.T) {
			var body map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&body)
				http.Error(w, "stop", 400)
			}))
			defer srv.Close()
			p := &Responses{Client: Client{BaseURL: srv.URL, APIKey: "fake", Model: "gpt-5.5", Thinking: setting}}
			_, _ = p.Chat(context.Background(), Request{}, nil)
			if body["thinking"] != nil || body["reasoning_effort"] != nil {
				t.Fatalf("foreign fields: %v", body)
			}
			switch setting {
			case ThinkingOn, ThinkingOff:
				want := "low"
				if setting == ThinkingOff {
					want = "none"
				}
				if got, _ := body["reasoning"].(map[string]any); got["effort"] != want {
					t.Fatalf("reasoning %v", body)
				}
			default:
				if body["reasoning"] != nil {
					t.Fatalf("unexpected control: %v", body)
				}
			}
		})
	}
}

func TestReasoningRedactionAndForeignProjection(t *testing.T) {
	m := Message{Role: RoleAssistant, Content: append([]ContentPart{reasoningPart("redact-me")}, TextContent("answer")...)}
	clean := m.MapText(func(s string) string { return strings.ReplaceAll(s, "redact-me", "[secret]") })
	if got, _ := replayReasoning(clean); got != "[secret]" {
		t.Fatalf("reasoning not redacted: %q", got)
	}
	if got, _ := replayReasoning(m); got != "redact-me" {
		t.Fatal("modified shared message")
	}
	_, a, err := anthropicInput([]Message{m}, true)
	if err != nil {
		t.Fatal(err)
	}
	r, err := responseInput([]Message{m})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []any{a, r} {
		raw, _ := json.Marshal(v)
		if strings.Contains(string(raw), "redact-me") || !strings.Contains(string(raw), "answer") {
			t.Fatalf("foreign projection: %s", raw)
		}
	}
}

func TestThinkingEndpointRoundTrip(t *testing.T) {
	for _, protocol := range []string{ChatCompletions, AnthropicMessages, ResponsesProtocol} {
		p, err := New(Endpoint{Protocol: protocol, Model: "fake", Thinking: ThinkingOn, ReasoningEffort: "high"})
		if err != nil {
			t.Fatal(err)
		}
		ep, ok := EndpointOf(WithoutHostedSearch(p))
		if !ok || ep.Thinking != ThinkingOn || ep.ReasoningEffort != "high" {
			t.Fatalf("lost thinking controls for %s", protocol)
		}
	}
}
