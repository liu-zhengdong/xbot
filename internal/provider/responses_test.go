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

func TestResponsesCompletedWithoutOutputRetainsStream(t *testing.T) {
	for _, tail := range []string{`"output":[],`, ""} {
		stream := `data: {"type":"response.output_text.delta","delta":"我是助手。"}

data: {"type":"response.output_item.done","output_index":1,"item":{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"我是助手。"}]}}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_1","encrypted_content":"opaque"}}

data: {"type":"response.completed","response":{"status":"completed",` + tail + `"usage":{"input_tokens":12,"output_tokens":4}}}

`
		msg, err := readResponses(context.Background(), strings.NewReader(stream), nil)
		if err != nil || msg.Text() != "我是助手。" || len(msg.Content) != 2 || msg.Content[0].Type != PartReasoning || !msg.Usage.Reported {
			t.Fatalf("lost completed items: %+v, %v", msg, err)
		}
		input, err := responseInput([]Message{msg})
		if err != nil || len(input) != 2 || !strings.Contains(string(input[1]), `"id":"msg_1"`) {
			t.Fatalf("native replay lost: %s %v", input, err)
		}
	}
}

func TestResponsesDeltaOnlyCompletionAndEmpty(t *testing.T) {
	for _, text := range []string{"已经收到。", ""} {
		stream := `data: {"type":"response.output_text.delta","delta":"` + text + `"}` + "\n\n" +
			`data: {"type":"response.completed","response":{"status":"completed","output":[]}}` + "\n\n"
		msg, err := readResponses(context.Background(), strings.NewReader(stream), nil)
		if text == "" {
			if err == nil {
				t.Fatal("empty assistant accepted as a successful response")
			}
		} else if err != nil || msg.Text() != text {
			t.Fatalf("streamed answer lost: %+v %v", msg, err)
		}
	}
}

func TestResponsesCompletedItemsKeepCallsAndBounds(t *testing.T) {
	item := `data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"web_fetch","arguments":"{}"}}` + "\n\n"
	end := `data: {"type":"response.completed","response":{"status":"completed","output":[]}}` + "\n\n"
	msg, err := readResponses(context.Background(), strings.NewReader(item+item+end), nil)
	if err != nil || len(msg.Calls()) != 1 {
		t.Fatalf("lost or duplicated local call: %+v %v", msg, err)
	}
	// Each event is below the scanner cap, but their retained sum is not.
	var oversized strings.Builder
	for i := 0; i < 3; i++ {
		fmt.Fprintf(&oversized, "data: {\"type\":\"response.output_item.done\",\"output_index\":%d,\"item\":{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":%q}]}}\n\n", i, strings.Repeat("x", 400<<10))
	}
	oversized.WriteString(end)
	if _, err := readResponses(context.Background(), strings.NewReader(oversized.String()), nil); err == nil {
		t.Fatal("unbounded retained output accepted")
	}
}

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

func TestResponsesStreamErrorsRetainRetryAndOverflowClassification(t *testing.T) {
	for _, code := range []string{"server_error", "rate_limit_exceeded", "context_length_exceeded"} {
		t.Run(code, func(t *testing.T) {
			body := `data: {"type":"response.failed","response":{"error":{"code":"` + code + `","message":"request failed"}}}`
			_, err := readResponses(context.Background(), strings.NewReader(body), nil)
			if code == "context_length_exceeded" {
				if !IsContextOverflow(err) {
					t.Fatalf("lost overflow: %v", err)
				}
			} else if !retryable(err) {
				t.Fatalf("lost retry classification: %v", err)
			}
		})
	}
	_, err := readResponses(context.Background(), strings.NewReader(`data: {"type":"error","code":"rate_limit_exceeded","message":"retry"}`), nil)
	if !retryable(err) {
		t.Fatalf("top-level error lost: %v", err)
	}
}

func TestTextEditsMapCitationOffsetsWithoutChangingOriginal(t *testing.T) {
	original := Message{Content: []ContentPart{{Type: PartText, Text: "reply_to: 7\n一。二。"}, {Type: PartCitation, Citation: &Citation{Start: 12, End: 14}}, {Type: PartCitation, Citation: &Citation{Start: 14, End: 16}}}}
	edited := original
	edited.SetText("一。二。")
	if edited.Content[1].Citation.Start != 0 || edited.Content[1].Citation.End != 2 || edited.Content[2].Citation.End != 4 {
		t.Fatalf("offsets %+v %+v", edited.Content[1].Citation, edited.Content[2].Citation)
	}
	if original.Content[1].Citation.Start != 12 {
		t.Fatal("edited shared transcript citation")
	}
}

func TestCitationInsertionKeepsExclusiveEnd(t *testing.T) {
	original := Message{Content: []ContentPart{{Type: PartText, Text: "abc"}, {Type: PartCitation, Citation: &Citation{Start: 0, End: 3}}}}
	after := original
	after.SetText("abc reminder")
	if after.Content[1].Citation.End != 3 {
		t.Fatalf("appended reminder became citation: %+v", after.Content[1].Citation)
	}
	before := original
	before.SetText("prefixabc")
	if before.Content[1].Citation.Start != 6 || before.Content[1].Citation.End != 9 {
		t.Fatalf("prefix citation %+v", before.Content[1].Citation)
	}
}

type captureTransport struct {
	bodies []map[string]any
}

func (c *captureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil {
		return nil, fmt.Errorf("bad request body")
	}
	c.bodies = append(c.bodies, body)
	sse := "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"id\":\"m1\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}]}}\n\n"
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(sse)), Header: http.Header{"Content-Type": []string{"text/event-stream"}}}, nil
}

func toolTypes(body map[string]any) []string {
	var out []string
	tools, _ := body["tools"].([]any)
	for _, t := range tools {
		if m, ok := t.(map[string]any); ok {
			out = append(out, fmt.Sprint(m["type"]))
		}
	}
	return out
}

func TestResponsesMountsXAIHostedTools(t *testing.T) {
	cases := []struct {
		name   string
		base   string
		hosted bool
		want   []string
	}{
		{"xai mounted", "https://api.x.ai/v1", true, []string{"function", "web_search", "x_search", "code_interpreter"}},
		{"xai hosted off", "https://api.x.ai/v1", false, []string{"function"}},
		{"openai stays web_search only", "https://api.openai.com/v1", true, []string{"function", "web_search"}},
		{"xai case-insensitive host", "https://API.X.AI/v1", true, []string{"function", "web_search", "x_search", "code_interpreter"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &captureTransport{}
			c := &Responses{Client: Client{BaseURL: tc.base, APIKey: "fake", Model: "grok-4", HTTPClient: &http.Client{Transport: tr}}, WebSearch: tc.hosted}
			if _, err := c.Chat(context.Background(), Request{HostedSearch: tc.hosted, Tools: []ToolDef{{Name: "shell"}}}, nil); err != nil {
				t.Fatal(err)
			}
			if len(tr.bodies) != 1 {
				t.Fatalf("requests %d", len(tr.bodies))
			}
			got := toolTypes(tr.bodies[0])
			if !reflect.DeepEqual(got, tc.want) && !(len(got) == 0 && len(tc.want) == 0) {
				t.Fatalf("tools %v want %v", got, tc.want)
			}
		})
	}
}

func TestResponsesParsesXAIHostedCalls(t *testing.T) {
	for _, event := range []struct{ event, tool, status string }{
		{"response.x_search_call.searching", "x_search", "searching"},
		{"response.code_interpreter_call.in_progress", "code_interpreter", "in_progress"},
		{"response.code_interpreter_call.completed", "code_interpreter", "completed"},
		{"response.output_text.delta", "", ""},
		{"response.file_search_call.in_progress", "", ""},
	} {
		tool, status, ok := hostedProgress(event.event)
		if event.tool == "" && ok {
			t.Fatalf("%s mistaken for a hosted call", event.event)
		}
		if event.tool != "" && (!ok || tool != event.tool || status != event.status) {
			t.Fatalf("%s -> %s %s %v", event.event, tool, status, ok)
		}
	}
	stream := `data: {"type":"response.x_search_call.searching","item_id":"xs_1"}

data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"x_search_call","id":"xs_1","status":"completed","action":{"type":"x_search","query":"最新消息"}},{"type":"code_interpreter_call","id":"ci_1","status":"completed"},{"type":"collections_search_call","id":"cs_1","status":"completed"},{"type":"message","id":"m1","content":[{"type":"output_text","text":"答案"}]}]}}

`
	msg, err := readResponses(context.Background(), strings.NewReader(stream), nil)
	if err != nil {
		t.Fatal(err)
	}
	var hosted []string
	for _, p := range msg.Content {
		if p.Type == PartHosted && p.Hosted != nil {
			hosted = append(hosted, p.Hosted.Name+":"+p.Hosted.Status)
		}
	}
	if !reflect.DeepEqual(hosted, []string{"x_search:completed", "code_interpreter:completed"}) {
		t.Fatalf("hosted steps %v", hosted)
	}
	if first := msg.Content[0]; first.Hosted == nil || first.Hosted.Query != "最新消息" {
		t.Fatalf("x_search query lost: %+v", msg.Content[0])
	}
	// Unknown hosted call types degrade to text, never a fake hosted step.
	unknown := msg.Content[2]
	if unknown.Type == PartHosted || !strings.Contains(unknown.Text, "collections_search_call") {
		t.Fatalf("unknown call not degraded: %+v", unknown)
	}
	if !strings.Contains(msg.PortableText(), "X 搜索") || !strings.Contains(msg.PortableText(), "代码解释器") {
		t.Fatalf("portable summaries lost: %s", msg.PortableText())
	}
}

func TestResponsesParsesRealXSearchCustomCalls(t *testing.T) {
	// xAI's current X search output: custom_tool_call items named after the
	// real function, distinguished by the xs_ call-id prefix.
	stream := `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"xs_9","call_id":"xs_9","name":"x_keyword_search","status":"in_progress","input":"{\"query\":\"grok 4 发布\"}"}}

data: {"type":"response.output_text.delta","delta":"搜到了。"}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call","id":"xs_9","call_id":"xs_9","name":"x_keyword_search","status":"completed","input":"{\"query\":\"grok 4 发布\"}"}}

data: {"type":"response.output_item.done","output_index":1,"item":{"type":"custom_tool_call","id":"ct_2","call_id":"ct_2","name":"spreadsheet_calc","status":"completed","input":"{}"}}

data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"custom_tool_call","id":"xs_9","call_id":"xs_9","name":"x_keyword_search","status":"completed","input":"{\"query\":\"grok 4 发布\"}"},{"type":"custom_tool_call","id":"ct_2","call_id":"ct_2","name":"spreadsheet_calc","status":"completed","input":"{}"},{"type":"message","id":"m1","content":[{"type":"output_text","text":"答案"}]}]}}

`
	msg, err := readResponses(context.Background(), strings.NewReader(stream), nil)
	if err != nil {
		t.Fatal(err)
	}
	var hosted []string
	for _, p := range msg.Content {
		if p.Type == PartHosted && p.Hosted != nil {
			hosted = append(hosted, p.Hosted.Name+":"+p.Hosted.Status+":"+p.Hosted.ID)
		}
	}
	if !reflect.DeepEqual(hosted, []string{"x_search:completed:xs_9"}) {
		t.Fatalf("hosted steps %v", hosted)
	}
	if step := msg.Content[0]; step.Hosted == nil || step.Hosted.Query != "grok 4 发布" {
		t.Fatalf("x_search query lost: %+v", msg.Content[0])
	}
	// A hosted X search never becomes a local tool call.
	if len(msg.Calls()) != 0 {
		t.Fatalf("custom call entered the local executor: %+v", msg.Calls())
	}
	// Other custom tools degrade to text, not fake hosted steps.
	other := msg.Content[1]
	if other.Type == PartHosted || other.Hosted != nil || !strings.Contains(other.Text, "custom_tool_call") {
		t.Fatalf("other custom call misclassified: %+v", other)
	}
	if !strings.Contains(msg.PortableText(), "X 搜索") {
		t.Fatalf("portable summary lost: %s", msg.PortableText())
	}

	// A failed X search stays a failed hosted step, never a success.
	failed := `data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"custom_tool_call","id":"xs_bad","call_id":"xs_bad","name":"x_keyword_search","status":"failed","input":"{\"query\":\"x\"}"},{"type":"message","id":"m1","content":[{"type":"output_text","text":"没搜到"}]}]}}

`
	msg, err = readResponses(context.Background(), strings.NewReader(failed), nil)
	if err != nil {
		t.Fatal(err)
	}
	step := msg.Content[0]
	if step.Type != PartHosted || step.Hosted == nil || step.Hosted.Status != "failed" || step.Hosted.Name != "x_search" {
		t.Fatalf("failed x_search lost: %+v", step)
	}
}
