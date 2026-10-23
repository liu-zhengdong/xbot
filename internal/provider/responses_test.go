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

const responseRecording = `data: {"type":"response.output_item.added","item":{"type":"web_search_call","id":"ws_1","status":"in_progress"}}

data: {"type":"response.web_search_call.searching","item_id":"ws_1"}

data: {"type":"response.output_text.delta","delta":"根据来源，今天晴。"}

data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"opaque"},{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","query":"天气"}},{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"根据来源，今天晴。","annotations":[{"type":"url_citation","start_index":0,"end_index":9,"url":"https://example.org/weather","title":"天气来源"}]}]}],"usage":{"input_tokens":120,"output_tokens":23,"input_tokens_details":{"cached_tokens":17}}}}

`

func TestResponsesRecordedStreamAndNativeReplay(t *testing.T) {
	var request map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("wrong protocol path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, responseRecording)
	}))
	defer server.Close()
	c := &Responses{Client: Client{BaseURL: server.URL + "/v1", APIKey: "fake", Model: "gpt-5.5"}, WebSearch: true}
	var streamed string
	var steps []ContentPart
	ctx := WithPartObserver(context.Background(), func(p ContentPart) { steps = append(steps, p) })
	msg, err := c.Chat(ctx, Request{Messages: []Message{{Role: RoleUser, Content: TextContent("天气")}}, Tools: []ToolDef{{Name: "web_fetch", Parameters: map[string]any{"type": "object"}}}}, func(s string) { streamed += s })
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := request["messages"]; exists {
		t.Fatal("Completions request leaked")
	}
	if !strings.Contains(string(request["tools"]), `"type":"web_search"`) || strings.Contains(string(request["tools"]), `"name":"web_search"`) {
		t.Fatalf("tools %s", request["tools"])
	}
	if len(steps) != 2 || streamed != msg.Text() || len(msg.Calls()) != 0 {
		t.Fatalf("stream %q, steps %d, calls %+v", streamed, len(steps), msg.Calls())
	}
	if len(msg.Content) != 4 || msg.Content[1].Hosted.Query != "天气" || msg.Content[3].Citation.Title != "天气来源" {
		t.Fatalf("blocks %+v", msg.Content)
	}
	if !msg.Usage.Reported || msg.Usage.CachedTokens != 17 {
		t.Fatalf("usage %+v", msg.Usage)
	}
	raw, _ := json.Marshal(msg)
	var stored Message
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(msg.Content, stored.Content) {
		t.Fatal("lost persisted content")
	}
	input, err := responseInput([]Message{stored})
	if err != nil {
		t.Fatal(err)
	}
	if len(input) != 3 || !strings.Contains(string(input[0]), `"encrypted_content":"opaque"`) || !strings.Contains(string(input[1]), `"web_search_call"`) || !strings.Contains(string(input[2]), `"annotations"`) {
		t.Fatalf("native replay %s", input)
	}
	wire, err := toWire(stored)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(wire.Content), "厂商搜索") || !strings.Contains(string(wire.Content), "天气来源") || len(wire.ToolCalls) != 0 {
		t.Fatalf("foreign projection %+v", wire)
	}
}

func TestResponsesLocalFunctionRoundTripAndImages(t *testing.T) {
	parts, err := responseParts(json.RawMessage(`{"type":"function_call","id":"fc_1","call_id":"call_1","name":"web_fetch","arguments":"{\"url\":\"https://example.org\"}"}`))
	if err != nil {
		t.Fatal(err)
	}
	msg := Message{Role: RoleAssistant, Content: parts}
	if len(msg.Calls()) != 1 {
		t.Fatal("missing local call")
	}
	input, err := responseInput([]Message{msg, {Role: RoleTool, Content: ToolResult("call_1", "page")}, {Role: RoleUser, Content: TextContent("看图"), Images: []Image{{MIME: "image/png", Data: []byte("png")}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(input) != 3 || !strings.Contains(string(input[1]), `"type":"function_call_output"`) || !strings.Contains(string(input[1]), `"call_id":"call_1"`) || !strings.Contains(string(input[2]), `"type":"input_image"`) {
		t.Fatalf("input %s", input)
	}
}

func TestResponsesRejectsDoubleSearchAndBrokenStreams(t *testing.T) {
	c := &Responses{Client: Client{BaseURL: "http://127.0.0.1:1", APIKey: "fake", Model: "gpt-5.5"}, WebSearch: true}
	if _, err := c.Chat(context.Background(), Request{Tools: []ToolDef{{Name: "web_search"}}}, nil); err == nil || !strings.Contains(err.Error(), "coexist") {
		t.Fatalf("double search accepted: %v", err)
	}
	for _, stream := range []string{`data: {bad}`, `data: {"type":"response.failed"}`, `data: {"type":"response.incomplete"}`, `data: {"type":"response.output_text.delta","delta":"partial"}`} {
		if _, err := readResponses(context.Background(), strings.NewReader(stream), nil); err == nil {
			t.Fatalf("accepted broken stream %s", stream)
		}
	}
}

func TestResponsesRetriesAndBoundsIdle(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(429)
			return
		}
		fmt.Fprint(w, responseRecording)
	}))
	defer server.Close()
	c := &Responses{Client: Client{BaseURL: server.URL, APIKey: "fake", Model: "gpt-5.4", retryWait: time.Millisecond}}
	if _, err := c.Chat(context.Background(), Request{}, nil); err != nil || attempts != 2 {
		t.Fatalf("retry: %d %v", attempts, err)
	}
	idle := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer idle.Close()
	c.BaseURL = idle.URL
	c.StreamIdleTimeout = 10 * time.Millisecond
	if _, err := c.Chat(context.Background(), Request{}, nil); err == nil || !strings.Contains(err.Error(), "idle") {
		t.Fatalf("idle not bounded: %v", err)
	}
}

func TestResponsesMultipleMessagesCitationOffsetsAndMissingUsage(t *testing.T) {
	stream := `data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"先说明。","annotations":[]}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"引用。","annotations":[{"type":"url_citation","start_index":0,"end_index":3,"url":"https://example.org","title":"来源"}]}]}]}}`
	msg, err := readResponses(context.Background(), strings.NewReader(stream), nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content[2].Citation.Start != 4 || msg.Content[2].Citation.End != 7 {
		t.Fatalf("citation offset %+v", msg.Content[2].Citation)
	}
	if msg.Usage.Reported {
		t.Fatal("missing usage claimed as reported")
	}
}

func TestResponsesNoRetryAfterHostedProgress(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		io.WriteString(w, `data: {"type":"response.output_item.added","item":{"type":"web_search_call","id":"ws_1","status":"in_progress"}}`+"\n\n")
	}))
	defer server.Close()
	c := &Responses{Client: Client{BaseURL: server.URL, APIKey: "fake", Model: "gpt-5.5", retryWait: time.Millisecond}, WebSearch: true}
	if _, err := c.Chat(context.Background(), Request{HostedSearch: true}, nil); err == nil || attempts != 1 {
		t.Fatalf("replayed visible hosted call: %d %v", attempts, err)
	}
}

func TestResponsesToolImagesUseUserRoleAndExistingMultimodalBudget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if len(raw) <= 1<<20 {
			t.Error("fixture did not exceed 1 MiB")
		}
		var req struct {
			Input []struct {
				Role string `json:"role"`
				Type string `json:"type"`
			} `json:"input"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Error(err)
		}
		if len(req.Input) != 2 || req.Input[0].Type != "function_call_output" || req.Input[1].Role != RoleUser {
			t.Errorf("image roles %+v", req.Input)
		}
		io.WriteString(w, responseRecording)
	}))
	defer server.Close()
	c := &Responses{Client: Client{BaseURL: server.URL, APIKey: "fake", Model: "gpt-5.5"}}
	_, err := c.Chat(context.Background(), Request{Messages: []Message{{Role: RoleTool, Content: ToolResult("call_1", "图片"), Images: []Image{{MIME: "image/png", Data: make([]byte, 800<<10)}}}}}, nil)
	if err != nil {
		t.Fatalf("existing image budget rejected: %v", err)
	}
}

func TestResponsesToolImageRole(t *testing.T) {
	input, err := responseInput([]Message{{Role: RoleTool, Content: ToolResult("call_1", "image"), Images: []Image{{MIME: "image/png", Data: []byte("small")}}}})
	if err != nil {
		t.Fatal(err)
	}
	var image struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(input[1], &image); err != nil {
		t.Fatal(err)
	}
	if image.Role != RoleUser {
		t.Fatalf("Responses image role %q", image.Role)
	}
}
