package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/provider"
)

// User downloads share the public projection for both formats and both tool
// settings. Native replay payloads remain in the internal segment unchanged.
func TestExportHidesThinkingAcrossProtocols(t *testing.T) {
	for protocol, reasoning := range map[string][]provider.ContentPart{
		provider.ChatCompletions: {{Type: provider.PartReasoning, Text: "PRIVATE-thought", Vendor: provider.ChatCompletions, VendorRaw: json.RawMessage(`{"reasoning_content":"PRIVATE-completions"}`)}},
		provider.AnthropicMessages: {
			{Type: provider.PartReasoning, Text: "PRIVATE-thought", Vendor: provider.AnthropicMessages, VendorRaw: json.RawMessage(`{"type":"thinking","thinking":"PRIVATE-anthropic","signature":"PRIVATE-signature"}`)},
			{Type: provider.PartReasoning, Vendor: provider.AnthropicMessages, VendorRaw: json.RawMessage(`{"type":"redacted_thinking","data":"PRIVATE-redacted"}`)},
		},
		provider.ResponsesProtocol: {{Type: provider.PartReasoning, Text: "PRIVATE-thought", Vendor: "openai_responses", VendorRaw: json.RawMessage(`{"type":"reasoning","id":"rs1","encrypted_content":"PRIVATE-encrypted","summary":[]}`)}},
	} {
		t.Run(protocol, func(t *testing.T) {
			s := &Server{Root: t.TempDir(), Owner: "u1"}
			token, _, err := s.EnsureToken()
			if err != nil {
				t.Fatal(err)
			}
			b, err := bot.Create(s.Root, "ada", "persona", nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := b.SetOwners([]string{"u1"}); err != nil {
				t.Fatal(err)
			}
			defer bot.CloseStore(b)
			call := provider.ToolCall{ID: "call1", Type: "function", Function: provider.FunctionCall{Name: "shell", Arguments: `{"command":"pwd"}`}}
			public := []provider.ContentPart{
				{Type: provider.PartText, Text: "普通答案", Vendor: protocol, VendorRaw: json.RawMessage(`{"text":"普通答案","extra":"PRIVATE-text"}`)},
				{Type: provider.PartHosted, Text: "公开搜索步骤", Hosted: &provider.HostedStep{ID: "host1", Name: "web_search", Status: "completed", Query: "公开查询"}, VendorRaw: json.RawMessage(`{"internal":"PRIVATE-hosted"}`)},
				{Type: provider.PartCitation, Citation: &provider.Citation{URL: "https://example.org/source", Title: "公开来源", Start: 0, End: 4}, VendorRaw: json.RawMessage(`{"internal":"PRIVATE-citation"}`)},
			}
			parts := append(append([]provider.ContentPart(nil), reasoning...), public...)
			parts = append(parts, provider.ContentPart{Type: provider.PartToolCall, ToolCall: &call, VendorRaw: json.RawMessage(`{"internal":"PRIVATE-call"}`)})
			result := provider.ToolResult("call1", "公开工具结果")
			result[0].VendorRaw = json.RawMessage(`{"internal":"PRIVATE-result"}`)
			for _, m := range []provider.Message{{Role: provider.RoleAssistant, Content: parts}, {Role: provider.RoleTool, Content: result}} {
				if err := b.Append(bot.Entry{Message: m}); err != nil {
					t.Fatal(err)
				}
			}
			files, err := b.SegmentFiles()
			if err != nil || len(files) != 1 {
				t.Fatalf("segments: %v %v", files, err)
			}
			before, err := os.ReadFile(files[0])
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(before, reasoning[0].VendorRaw) {
				t.Fatal("fixture lost internal reasoning")
			}
			ui := httptest.NewServer(muxOf(s))
			defer ui.Close()
			cookie := loginCookie(t, ui.URL, token)
			// The trace panel gets readable thinking text and nothing else private.
			var hist struct {
				Messages []apiRow `json:"messages"`
			}
			raw := getAuth(t, ui.URL+"/api/bots/ada/transcript", cookie)
			if err := json.Unmarshal(raw, &hist); err != nil || len(hist.Messages) != 2 {
				t.Fatalf("transcript: %v %s", err, raw)
			}
			want := map[string]string{provider.ChatCompletions: "PRIVATE-completions", provider.AnthropicMessages: "PRIVATE-anthropic"}[protocol]
			if hist.Messages[0].Thinking != want || hist.Messages[1].Thinking != "" {
				t.Fatalf("thinking = %q, want %q", hist.Messages[0].Thinking, want)
			}
			for _, forbidden := range []string{"vendor_raw", "reasoning_content", "signature", "encrypted_content", "redacted_thinking", `"type":"reasoning"`} {
				if bytes.Contains(raw, []byte(forbidden)) {
					t.Errorf("transcript contains %s: %s", forbidden, raw)
				}
			}
			for _, format := range []string{"jsonl", "md"} {
				for _, tools := range []int{0, 1} {
					t.Run(fmt.Sprintf("%s/tools=%d", format, tools), func(t *testing.T) {
						raw := getAuth(t, fmt.Sprintf("%s/api/bots/ada/export?format=%s&tools=%d", ui.URL, format, tools), cookie)
						for _, forbidden := range []string{"PRIVATE-", "vendor_raw", "reasoning_content", "signature", "encrypted_content", "redacted_thinking", `"type":"reasoning"`} {
							if bytes.Contains(raw, []byte(forbidden)) {
								t.Errorf("download contains %s: %s", forbidden, raw)
							}
						}
						if !bytes.Contains(raw, []byte("普通答案")) {
							t.Fatalf("answer lost: %s", raw)
						}
						if bytes.Contains(raw, []byte("公开工具结果")) != (tools == 1) {
							t.Fatalf("tool result visibility: %s", raw)
						}
						if format == "jsonl" {
							var sawHosted, sawCitation, sawCall bool
							for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
								var entry bot.Entry
								if err := json.Unmarshal([]byte(line), &entry); err != nil {
									t.Fatal(err)
								}
								for _, p := range entry.Message.Content {
									sawHosted = sawHosted || p.Type == provider.PartHosted && p.Hosted != nil && p.Hosted.Query == "公开查询"
									sawCitation = sawCitation || p.Type == provider.PartCitation && p.Citation != nil && p.Citation.Title == "公开来源" && p.Citation.URL == "https://example.org/source"
									sawCall = sawCall || p.Type == provider.PartToolCall && p.ToolCall != nil && p.ToolCall.Function.Name == "shell"
								}
							}
							if !sawHosted || !sawCitation || sawCall != (tools == 1) {
								t.Fatalf("public blocks lost: hosted=%t citation=%t call=%t", sawHosted, sawCitation, sawCall)
							}
						}
					})
				}
			}
			after, err := os.ReadFile(files[0])
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("public export rewrote internal transcript")
			}
			bot.CloseStore(b)
			reopened, err := b.Entries()
			if err != nil {
				t.Fatal(err)
			}
			for i, p := range reasoning {
				got := reopened[0].Message.Content[i]
				if got.Type != provider.PartReasoning || !bytes.Equal(got.VendorRaw, p.VendorRaw) {
					t.Fatal("reopen lost private replay payload")
				}
			}
		})
	}
}
