package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type continuationFunc func(context.Context, Request, func(string)) (Message, error)

func (f continuationFunc) Chat(ctx context.Context, req Request, delta func(string)) (Message, error) {
	return f(ctx, req, delta)
}

func TestCompleteContinuation(t *testing.T) {
	for _, reason := range []string{"length", "max_tokens", "max_output_tokens"} {
		t.Run(reason, func(t *testing.T) {
			calls, visible := 0, 0
			original := make([]Message, 1, 8)
			original[0] = Message{Role: RoleUser, Content: TextContent("question")}
			p := continuationFunc(func(ctx context.Context, req Request, d func(string)) (Message, error) {
				calls++
				if visible != 0 {
					t.Fatal("partial output escaped")
				}
				if calls == 2 {
					if len(req.Messages) != 3 || req.Messages[1].Text() != "前半" || !req.Messages[2].Internal || len(req.Tools) != 1 {
						t.Fatalf("continuation request: %+v", req)
					}
				}
				emitPart(ctx, ContentPart{Type: PartHosted, Hosted: &HostedStep{ID: "hidden-progress"}})
				text, stop := "前半", reason
				if calls == 2 {
					text, stop = "后半", "stop"
				}
				d(text)
				m := Message{Role: RoleAssistant, Content: TextContent(text), StopReason: stop, Usage: TokenUsage{Reported: true, PromptTokens: calls * 10, CompletionTokens: 2}}
				m.Content = append(m.Content, ContentPart{Type: PartCitation, Citation: &Citation{Start: 0, End: 2}})
				return m, nil
			})
			ctx := WithPartObserver(context.Background(), func(ContentPart) { visible++ })
			m, err := Complete(ctx, p, Request{Messages: original, Tools: []ToolDef{{Name: "read_file"}}}, func(s string) {
				visible++
				if s != "前半后半" {
					t.Fatalf("visible %q", s)
				}
			})
			if err != nil || calls != 2 || visible != 1 || m.Text() != "前半后半" || m.Truncated() {
				t.Fatalf("result %+v, %v; calls=%d visible=%d", m, err, calls, visible)
			}
			if m.Content[3].Citation.Start != 2 || m.Content[3].Citation.End != 4 {
				t.Fatal("citation offset lost")
			}
			if m.Usage.PromptTokens != 20 || m.Usage.InitialPromptTokens != 10 || m.Usage.Billable().PromptTokens != 30 {
				t.Fatalf("usage %+v", m.Usage)
			}
			if original[:cap(original)][1].Role != "" {
				t.Fatal("modified caller backing array")
			}
			raw, _ := json.Marshal(m)
			if strings.Contains(string(raw), "StopReason") || strings.Contains(string(raw), "stop_reason") {
				t.Fatal("persisted runtime reason")
			}
		})
	}
}

func TestCompleteBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		wantCalls    int
		wantErr      bool
	}{
		{"stop", "stop", 1, false}, {"end_turn", "end_turn", 1, false}, {"legacy", "", 1, false},
		{"limit", "length", 3, true}, {"failure", "length", 2, true}, {"cancel", "length", 2, true},
		{"oversized", "length", 1, true}, {"aggregate_size", "length", 2, true},
		{"empty", "length", 3, true}, {"tool", "length", 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls, visible := 0, 0
			p := continuationFunc(func(ctx context.Context, _ Request, d func(string)) (Message, error) {
				calls++
				if tc.name == "failure" && calls == 2 {
					return Message{}, errors.New("upstream failed")
				}
				if tc.name == "cancel" && calls == 2 {
					cancel()
					return Message{}, ctx.Err()
				}
				text := "part"
				if tc.name == "empty" {
					text = ""
				}
				if tc.name == "oversized" {
					text = strings.Repeat("x", maxContinuedOutput+1)
				}
				if tc.name == "aggregate_size" {
					text = strings.Repeat("x", maxContinuedOutput/2)
				}
				d(text)
				m := Message{Content: TextContent(text), StopReason: tc.reason}
				if tc.name == "tool" {
					m.Content = append(m.Content, ToolContent([]ToolCall{{ID: "c", Function: FunctionCall{Name: "shell", Arguments: "{"}}})...)
				}
				return m, nil
			})
			m, err := Complete(ctx, p, Request{}, func(string) { visible++ })
			if calls != tc.wantCalls || (err != nil) != tc.wantErr {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
			if tc.wantErr && (visible != 0 || len(m.Content) != 0) {
				t.Fatal("partial reply escaped on failure")
			}
			if !tc.wantErr && visible != 1 {
				t.Fatalf("visible=%d", visible)
			}
		})
	}
}

func TestJSONResponseBounds(t *testing.T) {
	for _, raw := range []string{`{bad`, `null`, `{"error":{"message":"private upstream data"}}`, strings.Repeat("x", (1<<20)+1), `{"choices":[{"message":{"tool_calls":[null]}}]}`} {
		if _, err := responseStream(strings.NewReader(raw), "application/json", ChatCompletions); err == nil || strings.Contains(err.Error(), "private upstream data") {
			t.Fatalf("invalid JSON error: %v", err)
		}
	}
}

func TestCompleteEmptyTruncationContinues(t *testing.T) {
	for _, reason := range []string{"length", "max_tokens", "max_output_tokens"} {
		calls := 0
		p := continuationFunc(func(_ context.Context, req Request, _ func(string)) (Message, error) {
			calls++
			if calls == 1 {
				return Message{Role: RoleAssistant, StopReason: reason}, nil
			}
			if len(req.Messages) != 1 || !req.Messages[0].Internal {
				t.Fatalf("invalid empty assistant replay: %+v", req.Messages)
			}
			return Message{Role: RoleAssistant, Content: TextContent("answer"), StopReason: "stop"}, nil
		})
		m, err := Complete(context.Background(), p, Request{}, nil)
		if err != nil || calls != 2 || m.Text() != "answer" {
			t.Fatalf("%s: %v calls=%d text=%q", reason, err, calls, m.Text())
		}
	}
}

func TestCompleteConcurrentRequestsStayIndependent(t *testing.T) {
	messages := make([]Message, 1, 8)
	messages[0] = Message{Role: RoleUser, Content: TextContent("question")}
	p := continuationFunc(func(_ context.Context, req Request, d func(string)) (Message, error) {
		if len(req.Messages) == 1 {
			d("prefix")
			return Message{Content: TextContent("prefix"), StopReason: "length"}, nil
		}
		if len(req.Messages) != 3 || req.Messages[1].Text() != "prefix" {
			return Message{}, errors.New("cross-request state")
		}
		d("suffix")
		return Message{Content: TextContent("suffix"), StopReason: "stop"}, nil
	})
	for i := 0; i < 8; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			m, err := Complete(context.Background(), p, Request{Messages: messages}, nil)
			if err != nil || m.Text() != "prefixsuffix" {
				t.Fatalf("%q %v", m.Text(), err)
			}
			if messages[:cap(messages)][1].Role != "" {
				t.Fatal("caller slice mutated")
			}
		})
	}
}
