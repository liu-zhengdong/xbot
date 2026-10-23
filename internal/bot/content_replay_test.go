package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"xbot/internal/provider"
)

// Exercises existing segment repair/index, append, reopen, export and the real
// Completions HTTP adapter. No separate storage or live model is involved.
func TestMixedTranscriptContentReplay(t *testing.T) {
	b, err := Create(t.TempDir(), "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Initialize the existing segment, then emulate rows written by an old binary.
	if err := b.Append(Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("seed")}}); err != nil {
		t.Fatal(err)
	}
	files, err := b.SegmentFiles()
	if err != nil || len(files) != 1 {
		t.Fatalf("segments %v %v", files, err)
	}
	CloseStore(b)
	old := []byte("{\"message\":{\"role\":\"user\",\"content\":\"旧话\"}}\n{\"message\":{\"role\":\"assistant\",\"content\":\"旧回答\"}}\n")
	f, err := os.OpenFile(files[0], os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(old); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	calls := []provider.ToolCall{{ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "shell", Arguments: `{"command":"pwd"}`}}}
	for _, m := range []provider.Message{
		{Role: provider.RoleAssistant, Content: append(provider.TextContent("新调用"), provider.ToolContent(calls)...)},
		{Role: provider.RoleTool, Name: "shell", Content: provider.ToolResult("c1", "新结果")},
	} {
		if err := b.Append(Entry{Message: m}); err != nil {
			t.Fatal(err)
		}
	}
	CloseStore(b)
	defer CloseStore(b)
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 || entries[1].Message.Text() != "旧话" || entries[4].Message.CallID() != "c1" {
		t.Fatalf("lost rows: %#v", entries)
	}
	after, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(after, before) {
		t.Fatal("rewrote old rows")
	}
	if !bytes.Contains(after[len(before):], []byte(`"content":[`)) {
		t.Fatal("new row is not blocks")
	}
	seqs, err := b.Search("新结果", 5)
	if err != nil || len(seqs) != 1 || seqs[0] != 5 {
		t.Fatalf("index: %v %v", seqs, err)
	}
	var received struct {
		Messages []struct {
			Role       string              `json:"role"`
			Content    string              `json:"content"`
			ToolCalls  []provider.ToolCall `json:"tool_calls"`
			ToolCallID string              `json:"tool_call_id"`
		} `json:"messages"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"继续回答\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	var messages []provider.Message
	for _, e := range entries {
		messages = append(messages, e.Message)
	}
	c := provider.Client{BaseURL: srv.URL, APIKey: "fake", Model: "deepseek-flash"}
	reply, err := c.Chat(context.Background(), provider.Request{Messages: messages}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(received.Messages) != 5 || received.Messages[1].Content != "旧话" || received.Messages[3].ToolCalls[0].ID != "c1" || received.Messages[4].ToolCallID != "c1" {
		t.Fatalf("request: %#v", received)
	}
	if err := b.Append(Entry{Message: reply}); err != nil {
		t.Fatal(err)
	}
	if reply.Text() != "继续回答" || len(reply.Content) != 1 {
		t.Fatalf("reply: %#v", reply)
	}
	var exported bytes.Buffer
	if err := ExportTo(&exported, b, TranscriptExport{Format: "jsonl", Tools: true}); err != nil {
		t.Fatal(err)
	}
	raw := exported.Bytes()
	if strings.Count(string(raw), "\n") != 6 || !bytes.Contains(raw, []byte("旧话")) || !bytes.Contains(raw, []byte(`"content":[`)) {
		t.Fatalf("export: %s", raw)
	}
	t.Logf("old rows retained byte-for-byte; rows 3 -> 6; request messages=%d; new content is ordered blocks", len(received.Messages))
}

func TestJSONLExportRedactsAcrossContentParts(t *testing.T) {
	b, err := Create(t.TempDir(), "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer CloseStore(b)
	m := provider.Message{Role: provider.RoleAssistant, Content: []provider.ContentPart{
		{Type: provider.PartText, Text: "before token-"},
		{Type: provider.PartToolCall, ToolCall: &provider.ToolCall{ID: "c1"}},
		{Type: provider.PartText, Text: "joined after"},
	}}
	if err := b.Append(Entry{Message: m}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := ExportTo(&out, b, TranscriptExport{Format: "jsonl", Tools: true, Redact: func(s string) string { return strings.ReplaceAll(s, "token-joined", "[secret]") }}); err != nil {
		t.Fatal(err)
	}
	var e Entry
	if err := json.Unmarshal(out.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	if e.Message.Text() != "before [secret] after" {
		t.Fatalf("JSONL reconstructs secret: %q", e.Message.Text())
	}
	if e.Message.Content[1].Type != provider.PartToolCall {
		t.Fatal("block order lost")
	}
}
