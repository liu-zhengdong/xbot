package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

// Exercise real protocol adapters, kernel publication and append-only storage.
// The server checks the visible surfaces before supplying the second half.
func TestOutputContinuationPublishesOnlyCompleteReply(t *testing.T) {
	for _, protocol := range []string{provider.ChatCompletions, provider.AnthropicMessages, provider.ResponsesProtocol} {
		for _, streaming := range []bool{true, false} {
			for _, prefix := range []string{"前半", ""} {
				t.Run(fmt.Sprintf("%s/stream=%t/empty=%t", protocol, streaming, prefix == ""), func(t *testing.T) {
					b := testBot(t, link.NameTerminal)
					if err := b.SetOwners([]string{"u1"}); err != nil {
						t.Fatal(err)
					}
					var mu sync.Mutex
					var visible []string
					calls := 0
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls++
						var req map[string]any
						if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
							t.Error(err)
						}
						mu.Lock()
						n := len(visible)
						mu.Unlock()
						if n != 0 {
							t.Error("partial text published before completion")
						}
						entries, err := b.Entries()
						if err != nil {
							t.Error(err)
						}
						for _, e := range entries {
							if e.Message.Role == provider.RoleAssistant {
								t.Error("partial assistant entry persisted")
							}
						}
						text := prefix
						if calls == 2 {
							text = "后半"
							body, _ := json.Marshal(req)
							if !strings.Contains(string(body), prefix) || !strings.Contains(string(body), "Continue exactly") {
								t.Errorf("continuation lost context: %s", body)
							}
						}
						if calls > 2 {
							t.Error("unexpected continuation")
						}
						if streaming {
							w.Header().Set("Content-Type", "text/event-stream")
						} else {
							w.Header().Set("Content-Type", "application/json")
						}
						switch protocol {
						case provider.ChatCompletions:
							stop := "length"
							if calls == 2 {
								stop = "stop"
							}
							if streaming {
								fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n", text, stop)
							} else {
								fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%q},"finish_reason":%q}]}`, text, stop)
							}
						case provider.AnthropicMessages:
							stop := "max_tokens"
							if calls == 2 {
								stop = "end_turn"
							}
							if streaming {
								fmt.Fprintf(w, "data: {\"type\":\"message_start\"}\n\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%q}}\n\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":%q}}\n\ndata: {\"type\":\"message_stop\"}\n\n", text, stop)
							} else {
								fmt.Fprintf(w, `{"content":[{"type":"text","text":%q}],"stop_reason":%q}`, text, stop)
							}
						case provider.ResponsesProtocol:
							status := "incomplete"
							if calls == 2 {
								status = "completed"
							}
							body := fmt.Sprintf(`{"status":%q,"incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":%q}]}]}`, status, text)
							if streaming {
								fmt.Fprintf(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":%q}\n\ndata: {\"type\":\"response.%s\",\"response\":%s}\n\n", text, status, body)
							} else {
								fmt.Fprint(w, body)
							}
						}
					}))
					defer srv.Close()
					client := provider.Client{BaseURL: srv.URL, APIKey: "fake-test-key", Model: "fake-model"}
					var p provider.Provider = &client
					if protocol == provider.AnthropicMessages {
						p = &provider.Anthropic{Client: client}
					}
					if protocol == provider.ResponsesProtocol {
						p = &provider.Responses{Client: client}
					}
					k := &Kernel{Bot: b, Provider: p, Memory: memory.NopStore{}, MaxSteps: 1, OnDelta: func(s string) { mu.Lock(); defer mu.Unlock(); visible = append(visible, s) }}
					if err := k.Handle(context.Background(), link.TerminalEvent("u1", "answer", time.Now())); err != nil {
						t.Fatal(err)
					}
					if calls != 2 {
						t.Fatalf("calls=%d", calls)
					}
					mu.Lock()
					defer mu.Unlock()
					if len(visible) != 1 || visible[0] != prefix+"后半" {
						t.Fatalf("visible: %q", visible)
					}
					entries, err := b.Entries()
					if err != nil {
						t.Fatal(err)
					}
					assistants := 0
					for _, e := range entries {
						if e.Message.Role == provider.RoleAssistant {
							assistants++
							if e.Message.Text() != prefix+"后半" {
								t.Fatalf("stored partial: %q", e.Message.Text())
							}
						}
						if strings.Contains(e.Message.Text(), "Continue exactly") {
							t.Fatal("continuation instruction persisted")
						}
					}
					if assistants != 1 {
						t.Fatalf("assistant entries=%d", assistants)
					}
				})
			}
		}
	}
}

func TestOutputContinuationFinalReplyGuards(t *testing.T) {
	for _, tc := range []struct {
		name, first, last, want string
		fail                    bool
	}{
		{"silent", "SI", "LENT", "", true},
		{"reply_marker", "reply_", "to: 1\nanswer", "answer", false},
		{"protocol", "<｜DS", "ML｜invoke name=\"shell\">bad</｜DSML｜invoke>", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := testBot(t, link.NameTerminal)
			if err := b.SetOwners([]string{"u1"}); err != nil {
				t.Fatal(err)
			}
			p := &scripted{resps: []provider.Message{
				{Role: provider.RoleAssistant, Content: provider.TextContent(tc.first), StopReason: "length"},
				{Role: provider.RoleAssistant, Content: provider.TextContent(tc.last), StopReason: "stop"},
			}}
			var visible strings.Builder
			k := &Kernel{Bot: b, Provider: p, MaxSteps: 1, OnDelta: func(s string) { visible.WriteString(s) }}
			err := k.Handle(context.Background(), link.TerminalEvent("u1", "hello", time.Now()))
			if (err != nil) != tc.fail {
				t.Fatalf("error=%v", err)
			}
			if tc.fail {
				if strings.Contains(visible.String(), tc.first+tc.last) {
					t.Fatal("invalid reply published")
				}
			} else if visible.String() != tc.want {
				t.Fatalf("visible=%q", visible.String())
			}
			entries, err := b.Entries()
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if e.Message.Role == provider.RoleAssistant && strings.Contains(e.Message.Text(), tc.first+tc.last) {
					t.Fatalf("unfiltered reply persisted: %q", e.Message.Text())
				}
			}
		})
	}
}

func TestOutputContinuationFailureDoesNotCommitPrefix(t *testing.T) {
	b := testBot(t, link.NameTerminal)
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	p := &scripted{resps: []provider.Message{{Role: provider.RoleAssistant, Content: provider.TextContent("PRIVATE_PARTIAL_PREFIX"), StopReason: "length"}}}
	var visible strings.Builder
	k := &Kernel{Bot: b, Provider: p, MaxSteps: 1, OnDelta: func(s string) { visible.WriteString(s) }}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "hello", time.Now())); err == nil {
		t.Fatal("continuation failure accepted")
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Message.Text(), "PRIVATE_PARTIAL_PREFIX") {
			t.Fatal("prefix persisted")
		}
	}
	if strings.Contains(visible.String(), "PRIVATE_PARTIAL_PREFIX") {
		t.Fatal("prefix published")
	}
}
