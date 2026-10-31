package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimitSelectorHTTP(t *testing.T) {
	for _, protocol := range []string{ChatCompletions, ResponsesProtocol, AnthropicMessages} {
		for _, backup := range []string{"", "ok", "limited"} {
			t.Run(protocol+"/"+backup, func(t *testing.T) {
				primaryCalls, backupCalls := 0, 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					primaryCalls++
					w.Header().Set("Retry-After", "7200")
					w.WriteHeader(429)
					fmt.Fprint(w, `{"error":{"type":"usage_limit_reached"}}`)
				}))
				defer server.Close()
				primary, err := New(Endpoint{Protocol: protocol, BaseURL: server.URL, APIKey: "test", Model: "primary"})
				if err != nil {
					t.Fatal(err)
				}
				s := RateLimitSelector{Primary: FixedSelector{P: primary}, MaxWait: 20 * time.Millisecond}
				if backup != "" {
					s.Fallback = FixedSelector{P: continuationFunc(func(context.Context, Request, func(string)) (Message, error) {
						backupCalls++
						if backup == "limited" {
							return Message{}, ErrRateLimitWait
						}
						return Message{Content: TextContent("flash result")}, nil
					})}
				}
				p, err := s.Select(context.Background(), Hint{})
				if err != nil {
					t.Fatal(err)
				}
				start := time.Now()
				msg, err := p.Chat(context.Background(), Request{}, nil)
				if backup == "ok" {
					if err != nil || msg.Text() != "flash result" {
						t.Fatalf("%+v %v", msg, err)
					}
					_, err = p.Chat(context.Background(), Request{}, nil)
					if err != nil || primaryCalls != 1 || backupCalls != 2 {
						t.Fatalf("did not stay on fallback: %d %d %v", primaryCalls, backupCalls, err)
					}
				} else if !errors.Is(err, ErrRateLimitWait) {
					t.Fatalf("want terminal quota error, got %v", err)
				}
				if primaryCalls != 1 || (backup == "limited" && backupCalls != 1) || (backup == "" && backupCalls != 0) {
					t.Fatalf("calls %d / %d", primaryCalls, backupCalls)
				}
				if time.Since(start) > time.Second {
					t.Fatal("waited on long reset")
				}
			})
		}
	}
}

func TestRateLimitFallbackKeepsLocalSearchContract(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"type":"message","role":"assistant","content":[{"type":"text","text":"backup"}],"stop_reason":"end_turn"}`)
	}))
	defer server.Close()
	backup := &Anthropic{Client: Client{BaseURL: server.URL, APIKey: "test", Model: "backup"}, WebSearch: true}
	primary := continuationFunc(func(context.Context, Request, func(string)) (Message, error) { return Message{}, ErrRateLimitWait })
	p, err := (RateLimitSelector{Primary: FixedSelector{P: primary}, Fallback: FixedSelector{P: backup}}).Select(context.Background(), Hint{})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := p.Chat(context.Background(), Request{Tools: []ToolDef{{Name: "web_search", Parameters: json.RawMessage(`{"type":"object"}`)}}}, nil)
	if err != nil || msg.Text() != "backup" || calls != 1 {
		t.Fatalf("local/hosted conflict: %v %v calls=%d", msg, err, calls)
	}
}

func TestRateLimitSuggestionsAndClassification(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, tc := range []struct {
		key, value string
		want       time.Duration
	}{
		{"Retry-After", "2", 2 * time.Second},
		{"Retry-After", now.Add(2 * time.Hour).Format(http.TimeFormat), 2 * time.Hour},
		{"X-RateLimit-Reset-Requests", "2s", 2 * time.Second},
		{"Anthropic-RateLimit-Tokens-Reset", now.Add(2 * time.Hour).Format(time.RFC3339), 2 * time.Hour},
		{"X-RateLimit-Reset", fmt.Sprint(now.Add(2 * time.Hour).Unix()), 2 * time.Hour},
	} {
		h := http.Header{}
		h.Set(tc.key, tc.value)
		if got := retryAfterHeader(h, now); got != tc.want {
			t.Errorf("%s: got %s want %s", tc.key, got, tc.want)
		}
	}
	for _, tc := range []struct {
		code    int
		body    string
		limited bool
	}{
		{400, `{"error":{"code":"usage_limit_reached","retry_after":7200}}`, true},
		{402, `{"error":{"code":"insufficient_quota"}}`, true},
		{401, `{"error":{"code":"usage_limit_reached"}}`, false},
		{403, `{"error":{"code":"usage_limit_reached"}}`, false},
		{400, `{"input":"usage_limit","error":{"message":"bad request"}}`, false},
		{500, `{"error":{"message":"temporary failure"}}`, false},
	} {
		d, limited := limitDelay(NewStatusError(tc.code, tc.body))
		if limited != tc.limited {
			t.Errorf("%d %s: limited=%v", tc.code, tc.body, limited)
		}
		if tc.code == 400 && tc.limited && d != 2*time.Hour {
			t.Errorf("body reset lost: %s", d)
		}
	}
}

func TestRateLimitSelectorCumulativeAndShort(t *testing.T) {
	for _, short := range []bool{false, true} {
		t.Run(fmt.Sprint(short), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if short && calls == 2 {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"recovered"},"finish_reason":"stop"}]}`)
					return
				}
				if short {
					w.Header().Set("Retry-After", "2")
				}
				w.WriteHeader(429)
				fmt.Fprint(w, `{"error":{"code":"rate_limit_exceeded"}}`)
			}))
			defer server.Close()
			c := &Client{BaseURL: server.URL, Model: "primary", APIKey: "test", retryWait: 5 * time.Millisecond}
			max := 12 * time.Millisecond
			if short {
				max = 3 * time.Second
			}
			p, err := (RateLimitSelector{Primary: FixedSelector{P: c}, MaxWait: max}).Select(context.Background(), Hint{})
			if err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			msg, err := p.Chat(context.Background(), Request{}, nil)
			if short {
				if err != nil || msg.Text() != "recovered" || calls != 2 || time.Since(start) < 2*time.Second {
					t.Fatalf("short reset: %v %v calls=%d", msg, err, calls)
				}
			} else if !errors.Is(err, ErrRateLimitWait) || calls != 3 {
				t.Fatalf("budget not enforced: %v calls=%d", err, calls)
			}
		})
	}
}
