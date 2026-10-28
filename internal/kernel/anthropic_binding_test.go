package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"xbot/internal/config"
	"xbot/internal/link"
	"xbot/internal/provider"
)

func TestAnthropicBindingToolRoundTripAndSwitch(t *testing.T) {
	b := testBot(t, link.NameTools, link.NameTerminal)
	claudeRequests, localResults := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Tools []struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"tools"`
			Messages []struct {
				Role    string            `json:"role"`
				Content []json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if r.URL.Path == "/messages" {
			claudeRequests++
			if err := json.Unmarshal(raw, &req); err != nil {
				t.Error(err)
			}
			hosted := 0
			for _, tool := range req.Tools {
				if tool.Name == "web_search" {
					if tool.Type != "web_search_20250305" {
						t.Error("local search leaked")
					}
					hosted++
				}
			}
			if hosted != 1 {
				t.Error("hosted schema missing")
			}
			fmt.Fprint(w, "data: {\"type\":\"message_start\"}\n\n")
			block := func(index int, value string) {
				fmt.Fprintf(w, "data: {\"type\":\"content_block_start\",\"index\":%d,\"content_block\":%s}\n\ndata: {\"type\":\"content_block_stop\",\"index\":%d}\n\n", index, value, index)
			}
			if claudeRequests == 1 {
				// Foreign Responses content must be portable text, never native output.
				if strings.Contains(string(raw), `"type":"web_search_call"`) {
					t.Error("foreign block leaked")
				}
				block(0, `{"type":"thinking","thinking":"private","signature":"signed"}`)
				block(1, `{"type":"server_tool_use","id":"srv1","name":"web_search","input":{"query":"news"}}`)
				block(2, `{"type":"web_search_tool_result","tool_use_id":"srv1","content":[]}`)
				block(3, `{"type":"tool_use","id":"fetch1","name":"web_fetch","input":{}}`)
				fmt.Fprint(w, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"}}\n\n")
			} else {
				for _, m := range req.Messages {
					for _, p := range m.Content {
						var part struct {
							Type    string `json:"type"`
							ID      string `json:"tool_use_id"`
							Content string `json:"content"`
						}
						_ = json.Unmarshal(p, &part)
						if part.Type == "tool_result" && part.ID == "fetch1" {
							if part.Content != "local" || m.Role != provider.RoleUser {
								t.Errorf("bad result %s", p)
							}
							localResults++
						}
					}
				}
				if !strings.Contains(string(raw), `"signature":"signed"`) {
					t.Error("thinking signature not replayed")
				}
				block(0, `{"type":"text","text":"Claude 已完成。"}`)
				fmt.Fprint(w, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n")
			}
			fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n\n")
		} else if r.URL.Path == "/responses" {
			if strings.Contains(string(raw), `"signature":"signed"`) {
				t.Error("signature leaked to GPT")
			}
			fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"web_search_call\",\"id\":\"ws1\",\"status\":\"completed\",\"action\":{\"query\":\"news\"}},{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"GPT 已完成。\"}]}]}}\n\n")
		} else {
			if strings.Contains(string(raw), `"signature":"signed"`) {
				t.Error("signature leaked to DeepSeek")
			}
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"本地绑定\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		}
	}))
	defer server.Close()
	on := true
	for _, name := range []string{"deepseek", "openai", "anthropic"} {
		m := config.ModelSpec{Name: "deepseek-flash", Context: 1000000}
		if name == "openai" {
			m.Name = "gpt-5.5"
			m.Context = 272000
		}
		if name == "anthropic" {
			m.Name = "claude-sonnet-4-5"
			m.Context = 200000
			m.HostedSearch = &on
		}
		if err := config.Add(b.Root, config.Provider{Name: name, BaseURL: server.URL, APIKeyEnv: "FAKE"}, m); err != nil {
			t.Fatal(err)
		}
	}
	fetch := 0
	k := &Kernel{OnTool: func(name, summary string) {
		if name == "web_fetch" {
			fetch++
		}
	}, Bot: b, Links: []link.Link{bindingSearchLink{}}, MaxSteps: 3, Now: func() time.Time { return time.Unix(1000, 0) }}
	for _, binding := range []struct {
		Name, Model string
		Window      int
	}{{"openai", "gpt-5.5", 272000}, {"anthropic", "claude-sonnet-4-5", 200000}, {"openai", "gpt-5.5", 272000}, {"deepseek", "deepseek-flash", 1000000}} {
		if err := b.SetBinding(binding.Name, binding.Model); err != nil {
			t.Fatal(err)
		}
		ep, _, err := config.Resolve(b.Root, binding.Name, binding.Model, func(string) string { return "fake" })
		if err != nil {
			t.Fatal(err)
		}
		k.Provider, err = provider.New(ep)
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		if err := k.Handle(context.Background(), link.TerminalEvent("owner", "search", time.Unix(1000, 0))); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s synthetic turn: %s", binding.Name, time.Since(start))
		if k.contextWindow() != binding.Window {
			t.Fatalf("stale context %d", k.contextWindow())
		}
		names := environmentGet[[]string](testPublished(t, k), "tools")
		if indexHas(testPublished(t, k), "web_search") != (binding.Name == "deepseek") || !slices.Contains(names, "web_fetch") {
			t.Fatalf("wrong tools %v", names)
		}
		next, err := k.sampleEnvironment(testPublished(t, k))
		if err != nil || !reflectToolsEqual(testPublished(t, k), next) {
			t.Fatalf("repeated tool update %v", err)
		}
	}
	if claudeRequests != 2 || localResults != 1 {
		t.Fatalf("tool round trip requests=%d results=%d", claudeRequests, localResults)
	}
	if len(k.shellJobs().List()) != 0 {
		t.Fatal("hosted entered Jobs")
	}
	traces, err := b.Traces()
	if err != nil {
		t.Fatal(err)
	}
	hosted := 0
	for _, tr := range traces {
		hosted += tr.HostedCalls

	}
	if hosted != 3 || fetch != 1 {
		t.Fatalf("duplicate/missing tools hosted=%d local=%d", hosted, fetch)
	}
}
