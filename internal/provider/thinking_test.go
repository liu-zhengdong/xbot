package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

// deepseek-flash thinks unless told not to, and XBot does not send
// reasoning_content back on tool turns, so every DeepSeek request must carry
// thinking disabled. Before this change no thinking field was sent at all.
func TestDeepSeekRequestDisablesThinking(t *testing.T) {
	for _, model := range []string{"deepseek-flash", "deepseek-chat", "deepseek-v4-pro", "group/auto-deepseek-v4-1-flash"} {
		got := requestThinking(t, &Client{Model: model})
		if got == nil || got["type"] != "disabled" {
			t.Fatalf("%s: thinking = %#v, want type disabled", model, got)
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
}

func TestNonDeepSeekAndReasonerSendNoThinking(t *testing.T) {
	for _, model := range []string{"mock", "gpt-4o", "deepseek-reasoner"} {
		if got := requestThinking(t, &Client{Model: model}); got != nil {
			t.Fatalf("%s: unexpected thinking %#v", model, got)
		}
	}
}
