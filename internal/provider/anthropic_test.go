package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Synthetic recording: no model account or API key is used.
const anthropicRecording = `data: {"type":"message_start","message":{"usage":{"input_tokens":20,"cache_read_input_tokens":7,"cache_creation_input_tokens":3,"output_tokens":1}}}

data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"private thought"}}

data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"opaque-signature"}}

data: {"type":"content_block_stop","index":0}

data: {"type":"content_block_start","index":1,"content_block":{"type":"server_tool_use","id":"srv_1","name":"web_search","input":{}}}

data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"天气\"}"}}

data: {"type":"content_block_stop","index":1}

data: {"type":"content_block_start","index":2,"content_block":{"type":"web_search_tool_result","tool_use_id":"srv_1","content":[{"type":"web_search_result","url":"https://example.org","title":"天气","encrypted_content":"opaque-result"}]}}

data: {"type":"content_block_stop","index":2}

data: {"type":"content_block_start","index":3,"content_block":{"type":"text","text":"","citations":[]}}

data: {"type":"content_block_delta","index":3,"delta":{"type":"text_delta","text":"晴天。"}}

data: {"type":"content_block_delta","index":3,"delta":{"type":"citations_delta","citation":{"type":"web_search_result_location","url":"https://example.org","title":"天气","encrypted_index":"opaque-index","cited_text":"晴天"}}}

data: {"type":"content_block_stop","index":3}

data: {"type":"content_block_start","index":4,"content_block":{"type":"future_block","payload":"opaque"}}

data: {"type":"content_block_stop","index":4}

data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":11}}

data: {"type":"message_stop"}

`

func TestAnthropicRecordedReplayProjectionAndEndpoint(t *testing.T) {
	var request map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "fake" || r.Header.Get("anthropic-version") != "2023-06-01" || r.Header.Get("Authorization") != "" {
			t.Error("wrong Messages route/auth")
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		io.WriteString(w, anthropicRecording)
	}))
	defer server.Close()
	p, err := New(Endpoint{Protocol: AnthropicMessages, BaseURL: server.URL + "/v1", APIKey: "fake", Model: "claude-sonnet-4-5", HostedSearch: true, Thinking: ThinkingOff})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	var steps []ContentPart
	msg, err := p.Chat(WithPartObserver(context.Background(), func(p ContentPart) { steps = append(steps, p) }), Request{Messages: []Message{{Role: RoleSystem, Content: TextContent("system")}, {Role: RoleUser, Content: TextContent("天气")}}, HostedSearch: true}, func(s string) { text += s })
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 3 || len(msg.Calls()) != 0 || text != "晴天。" || len(msg.Content) != 6 {
		t.Fatalf("parts %+v steps %d text %q", msg.Content, len(steps), text)
	}
	if msg.Usage.PromptTokens != 30 || msg.Usage.CachedTokens != 7 || msg.Usage.CompletionTokens != 11 || !msg.Usage.CacheReported {
		t.Fatalf("usage %+v", msg.Usage)
	}
	if !strings.Contains(string(request["tools"]), "web_search_20250305") || !strings.Contains(string(request["thinking"]), "disabled") || string(request["max_tokens"]) != "8192" || len(request["system"]) == 0 || request["input"] != nil {
		t.Fatalf("request %v", request)
	}
	raw, _ := json.Marshal(msg)
	var stored Message
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(msg.Content, stored.Content) {
		t.Fatal("lost stored blocks")
	}
	_, native, err := anthropicInput([]Message{stored}, true)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(native)
	for _, want := range []string{"opaque-signature", "opaque-result", "opaque-index", "Claude future_block 内容"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("replay missing %s: %s", want, b)
		}
	}
	if strings.Contains(string(b), `"type":"future_block"`) {
		t.Fatal("unknown native block replayed")
	}
	wire, err := toWire(stored, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(wire.Content), "厂商工具") || strings.Contains(string(wire.Content), "opaque-signature") || len(wire.ToolCalls) != 0 {
		t.Fatalf("foreign projection %+v", wire)
	}
	input, err := responseInput([]Message{stored})
	if err != nil {
		t.Fatal(err)
	}
	foreign, _ := json.Marshal(input)
	if strings.Contains(string(foreign), "opaque-signature") || !strings.Contains(string(foreign), "future_block") {
		t.Fatalf("Responses projection %s", foreign)
	}
	ep, ok := EndpointOf(p)
	if !ok || ep.Protocol != AnthropicMessages || !ep.HostedSearch || !HasHostedSearch(p) {
		t.Fatalf("endpoint %+v", ep)
	}
}

func TestAnthropicLocalToolProjectionAndImages(t *testing.T) {
	ps, err := anthropicParts(json.RawMessage(`{"type":"tool_use","id":"call_1","name":"web_fetch","input":{"url":"https://example.org"}}`), 0)
	if err != nil {
		t.Fatal(err)
	}
	msg := Message{Role: RoleAssistant, Content: ps}
	msg.SetCalls([]ToolCall{{ID: "call_new", Type: "function", Function: FunctionCall{Name: "web_fetch", Arguments: `{"url":"https://example.net"}`}}})
	sys, out, err := anthropicInput([]Message{{Role: RoleSystem, Content: TextContent("rules")}, msg, {Role: RoleTool, Content: ToolResult("call_new", "page"), Images: []Image{{MIME: "image/png", Data: []byte("png")}}}, {Role: RoleTool, Content: ToolResult("call_2", "other")}, {Role: RoleUser, Content: TextContent("continue")}}, true)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	if len(sys) != 1 || len(out) != 2 || len(out[1].Content) != 3 || strings.Contains(string(raw), "call_1") || !strings.Contains(string(raw), "call_new") || !strings.Contains(string(raw), `"type":"base64"`) || out[1].Role != RoleUser {
		t.Fatalf("projection %s", raw)
	}
}

func TestAnthropicErrorsRetriesAndNoDuplicateHosted(t *testing.T) {
	for _, stream := range []string{`data: {bad}`, `data: {"type":"message_stop"}`, `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"x"}}`, strings.Replace(anthropicRecording, `"stop_reason":"end_turn"`, `"stop_reason":"max_tokens"`, 1), strings.Replace(anthropicRecording, `"index":0,"content_block"`, `"index":2,"content_block"`, 1)} {
		if _, err := readAnthropic(context.Background(), strings.NewReader(stream), nil); err == nil {
			t.Fatalf("broken stream accepted %s", stream)
		}
	}
	for _, typ := range []string{"overloaded_error", "rate_limit_error"} {
		_, err := readAnthropic(context.Background(), strings.NewReader(`data: {"type":"error","error":{"type":"`+typ+`","message":"retry"}}`), nil)
		if !retryable(err) {
			t.Fatalf("lost retry classification %v", err)
		}
	}
	c := &Anthropic{Client: Client{BaseURL: "http://127.0.0.1:1", APIKey: "fake", Model: "claude"}, WebSearch: true}
	if _, err := c.Chat(context.Background(), Request{Tools: []ToolDef{{Name: "web_search"}}}, nil); err == nil || !strings.Contains(err.Error(), "coexist") {
		t.Fatalf("double search accepted %v", err)
	}
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(429)
			return
		}
		io.WriteString(w, anthropicRecording)
	}))
	defer server.Close()
	c.BaseURL = server.URL
	c.retryWait = time.Millisecond
	if _, err := c.Chat(context.Background(), Request{}, nil); err != nil || attempts != 2 {
		t.Fatalf("retry %d %v", attempts, err)
	}
	attempts = 0
	visible := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		fmt.Fprint(w, "data: {\"type\":\"message_start\"}\n\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"server_tool_use\",\"id\":\"srv\",\"name\":\"web_search\",\"input\":{}}}\n\n")
	}))
	defer visible.Close()
	c.BaseURL = visible.URL
	if _, err := c.Chat(context.Background(), Request{HostedSearch: true}, nil); err == nil || attempts != 1 {
		t.Fatalf("hosted replayed %d %v", attempts, err)
	}
	idle := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer idle.Close()
	c.BaseURL = idle.URL
	c.StreamIdleTimeout = 10 * time.Millisecond
	if _, err := c.Chat(context.Background(), Request{}, nil); err == nil || !strings.Contains(err.Error(), "idle") {
		t.Fatalf("unbounded idle %v", err)
	}
}

func TestAnthropicPauseContinuationAndLimits(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		raw, _ := io.ReadAll(r.Body)
		if attempts > 1 && !strings.Contains(string(raw), `"signature":"opaque-signature"`) {
			t.Error("pause history missing")
		}
		if attempts == 1 {
			io.WriteString(w, strings.Replace(anthropicRecording, `"stop_reason":"end_turn"`, `"stop_reason":"pause_turn"`, 1))
		} else {
			io.WriteString(w, anthropicRecording)
		}
	}))
	defer server.Close()
	c := &Anthropic{Client: Client{BaseURL: server.URL, APIKey: "fake", Model: "claude"}, WebSearch: true}
	msg, err := c.Chat(context.Background(), Request{HostedSearch: true}, nil)
	if err != nil || attempts != 2 || msg.Usage.PromptTokens != 30 || msg.Usage.CompletionTokens != 11 || msg.Usage.Billed == nil || msg.Usage.Billed.PromptTokens != 60 || msg.Usage.Billed.CompletionTokens != 22 || len(msg.Content) != 12 || msg.Content[10].Citation.Start != len([]rune("晴天。[Claude future_block 内容]")) {
		t.Fatalf("pause attempts=%d usage=%+v parts=%+v err=%v", attempts, msg.Usage, msg.Content, err)
	}
	tooLarge := strings.Replace(anthropicRecording, `"thinking":"private thought"`, `"thinking":"`+strings.Repeat("x", 1<<20)+`"`, 1)
	if _, err := readAnthropic(context.Background(), strings.NewReader(tooLarge), nil); err == nil {
		t.Fatal("oversized output accepted")
	}
	unknownDelta := strings.Replace(anthropicRecording, `data: {"type":"content_block_stop","index":4}`, `data: {"type":"content_block_delta","index":4,"delta":{"type":"future_delta","payload":"x"}}`+"\n\n"+`data: {"type":"content_block_stop","index":4}`, 1)
	if _, err := readAnthropic(context.Background(), strings.NewReader(unknownDelta), nil); err != nil {
		t.Fatalf("unknown block did not degrade: %v", err)
	}
}

func TestAnthropicToolImagesAreNestedAndIntegersExact(t *testing.T) {
	call := Message{Role: RoleAssistant, Content: ToolContent([]ToolCall{{ID: "a", Type: "function", Function: FunctionCall{Name: "image_tool", Arguments: `{"id":9007199254740993}`}}})}
	_, out, err := anthropicInput([]Message{call, {Role: RoleTool, Content: ToolResult("a", "picture"), Images: []Image{{MIME: "image/png", Data: []byte("png")}}}, {Role: RoleTool, Content: ToolResult("b", "other")}}, true)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	if !strings.Contains(string(raw), "9007199254740993") {
		t.Errorf("integer changed: %s", raw)
	}
	if len(out) != 2 || len(out[1].Content) != 2 {
		t.Fatalf("tool results interrupted by image: %s", raw)
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
		} `json:"content"`
	}
	if err := json.Unmarshal(out[1].Content[0], &result); err != nil || len(result.Content) != 2 || result.Content[1].Type != "image" {
		t.Fatalf("image outside tool_result: %s %v", out[1].Content[0], err)
	}
}

func TestAnthropicToolFreeSummaryFlattensToolHistory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		for _, typ := range []string{"tool_use", "tool_result", "server_tool_use", "web_search_tool_result", "web_search_20250305"} {
			if strings.Contains(string(raw), `"type":"`+typ+`"`) {
				t.Errorf("tool-free summary contains %s: %s", typ, raw)
			}
		}
		if !strings.Contains(string(raw), "web_fetch") || !strings.Contains(string(raw), "page") {
			t.Errorf("summary lost tool history: %s", raw)
		}
		io.WriteString(w, "data: {\"type\":\"message_start\"}\n\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"summary\"}}\n\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()
	c := &Anthropic{Client: Client{BaseURL: server.URL, APIKey: "fake", Model: "claude"}, WebSearch: true}
	parts, err := anthropicParts(json.RawMessage(`{"type":"server_tool_use","id":"srv","name":"web_search","input":{"query":"news"}}`), 0)
	if err != nil {
		t.Fatal(err)
	}
	parts = append(parts, ToolContent([]ToolCall{{ID: "fetch", Type: "function", Function: FunctionCall{Name: "web_fetch", Arguments: `{}`}}})...)
	_, err = c.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: TextContent("go")}, {Role: RoleAssistant, Content: parts}, {Role: RoleTool, Content: ToolResult("fetch", "page")}, {Role: RoleUser, Content: TextContent("summarize")}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestAnthropicDoesNotRedirectAPIKey(t *testing.T) {
	leaked := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("x-api-key") != ""
		io.WriteString(w, anthropicRecording)
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/messages", http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	c := &Anthropic{Client: Client{BaseURL: redirect.URL, APIKey: "fake", Model: "claude"}}
	_, err := c.Chat(context.Background(), Request{}, nil)
	if err == nil || leaked {
		t.Fatalf("redirect accepted or API key forwarded: err=%v leaked=%v", err, leaked)
	}
}

func TestAnthropicFailedSearchStatusAndSingleCount(t *testing.T) {
	stream := strings.Replace(anthropicRecording, `[{"type":"web_search_result","url":"https://example.org","title":"天气","encrypted_content":"opaque-result"}]`, `{"type":"web_search_tool_result_error","error_code":"unavailable"}`, 1)
	var steps []ContentPart
	msg, err := readAnthropic(WithPartObserver(context.Background(), func(p ContentPart) { steps = append(steps, p) }), strings.NewReader(stream), nil)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, p := range msg.Content {
		if p.Hosted != nil {
			count++
			if p.Hosted.Status != "failed" {
				t.Errorf("failed search stored as %s", p.Hosted.Status)
			}
		}
	}
	if count != 1 || len(steps) == 0 || steps[len(steps)-1].Hosted.Status != "failed" {
		t.Fatalf("count=%d progress=%+v", count, steps)
	}
}

func TestAnthropicImageLimitIsRecoverableForTool(t *testing.T) {
	img := Image{MIME: "image/png", Data: make([]byte, 8<<20)} // Base64 exceeds direct API's 10 MB cap.
	_, out, err := anthropicInput([]Message{{Role: RoleTool, Content: ToolResult("image1", "picture"), Images: []Image{img}}}, true)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Error   bool            `json:"is_error"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(out[0].Content[0], &result); err != nil {
		t.Fatal(err)
	}
	if !result.Error || strings.Contains(string(result.Content), `"type":"image"`) || !strings.Contains(string(result.Content), "10 MB") {
		t.Fatal("oversized tool image was not a recoverable error")
	}
	if _, _, err := anthropicInput([]Message{{Role: RoleUser, Images: []Image{img}}}, true); err == nil {
		t.Fatal("oversized owner image accepted")
	}
}

func TestAnthropicPauseReportsLastContextUsage(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			io.WriteString(w, strings.Replace(anthropicRecording, `"stop_reason":"end_turn"`, `"stop_reason":"pause_turn"`, 1))
		} else {
			io.WriteString(w, anthropicRecording)
		}
	}))
	defer server.Close()
	c := &Anthropic{Client: Client{BaseURL: server.URL, APIKey: "fake", Model: "claude"}, WebSearch: true}
	msg, err := c.Chat(context.Background(), Request{HostedSearch: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Usage.PromptTokens != 30 || msg.Usage.CompletionTokens != 11 || msg.Usage.CachedTokens != 7 {
		t.Fatalf("cumulative billing used as context: %+v", msg.Usage)
	}
}
