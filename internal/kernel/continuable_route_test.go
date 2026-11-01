package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/config"
	"xbot/internal/exec"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

func TestContinuableActualChildSearchAndVisionRoute(t *testing.T) {
	for _, childHosted := range []bool{false, true} {
		t.Run(fmt.Sprintf("child_hosted_%t", childHosted), func(t *testing.T) {
			var mu sync.Mutex
			var local, hosted int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var in struct {
					Tools []struct {
						Type     string `json:"type"`
						Name     string `json:"name"`
						Function struct {
							Name string `json:"name"`
						} `json:"function"`
					} `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
					t.Error(err)
				}
				l, h := 0, 0
				for _, tool := range in.Tools {
					if tool.Function.Name == "web_search" || tool.Name == "web_search" && tool.Type == "function" {
						l++
					}
					if strings.HasPrefix(tool.Type, "web_search") {
						h++
					}
				}
				mu.Lock()
				local += l
				hosted += h
				mu.Unlock()
				w.Header().Set("Content-Type", "text/event-stream")
				if r.URL.Path == "/responses" {
					fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"id\":\"m\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"done\"}]}]}}\n\n")
				} else {
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				}
			}))
			defer server.Close()
			t.Setenv("CHILD_ROUTE_KEY", "scripted-fixture")
			k, b, box := dispatchKernel(t, &roleModel{})
			for _, host := range []bool{false, true} {
				name, protocol := "plain", config.ChatCompletions
				if host {
					name, protocol = "hosted", config.Responses
				}
				if err := config.Add(b.Root, config.Provider{Name: name, Protocol: protocol, BaseURL: server.URL, APIKeyEnv: "CHILD_ROUTE_KEY"}, config.ModelSpec{Name: name, Context: 30000, HostedSearch: &host, Vision: &host}); err != nil {
					t.Fatal(err)
				}
			}
			child, parent := "plain", "hosted"
			if childHosted {
				child, parent = "hosted", "plain"
			}
			if err := b.SetBinding(parent, parent); err != nil {
				t.Fatal(err)
			}
			endpoint, _, err := config.Resolve(b.Root, parent, parent, config.Getenv)
			if err != nil {
				t.Fatal(err)
			}
			k.Provider, err = provider.New(endpoint)
			if err != nil {
				t.Fatal(err)
			}
			task, err := k.StartSubagent(context.Background(), SubagentRequest{Type: "research", Task: "compare sources", Provider: child, Model: child})
			if err != nil {
				t.Fatal(err)
			}
			waitEvent(t, box, exec.EvDone)
			d, err := exec.ReadDescriptor(b.AgentDir(task.ID))
			if err != nil {
				t.Fatal(err)
			}
			var image *tools.ViewImage
			for _, tool := range k.toolsFor(exec.StartRequest{ID: task.ID, Descriptor: &d}) {
				if v, ok := tool.(*tools.ViewImage); ok {
					image = v
				}
			}
			if image == nil || image.Vision != childHosted || image.Model != child {
				t.Fatalf("child inherited parent vision: %+v", image)
			}
			mu.Lock()
			l, h := local, hosted
			mu.Unlock()
			if childHosted && (l != 0 || h != 1) {
				t.Fatalf("hosted child request has local=%d hosted=%d", l, h)
			}
			if !childHosted && (l != 1 || h != 0) {
				t.Fatalf("plain child request has local=%d hosted=%d", l, h)
			}
		})
	}
}

func TestContinuableActualChildFitSummaryToolsAndOverflow(t *testing.T) {
	for _, scenario := range []string{"history", "tool_schema", "overflow"} {
		t.Run(scenario, func(t *testing.T) {
			var mu sync.Mutex
			calls, summaries := 0, 0
			window := 12000
			if scenario == "tool_schema" {
				window = 2000
			}
			_, ceiling := compact.Lines(window)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var in struct {
					Model    string `json:"model"`
					Messages []struct {
						Role    string `json:"role"`
						Content string `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
					t.Error(err)
				}
				mu.Lock()
				calls++
				n := calls
				mu.Unlock()
				if in.Model != "small-child" {
					t.Errorf("summary/chat used wrong model %q", in.Model)
				}
				msgs := make([]provider.Message, 0, len(in.Messages))
				for _, m := range in.Messages {
					msgs = append(msgs, provider.Message{Role: m.Role, Content: provider.TextContent(m.Content)})
				}
				if size := compact.EstimateMessages(msgs); size > ceiling {
					t.Errorf("child request escaped its hard ceiling: %d > %d", size, ceiling)
				}
				if scenario == "overflow" && n == 1 {
					w.WriteHeader(http.StatusRequestEntityTooLarge)
					fmt.Fprint(w, `{"error":"too large"}`)
					return
				}
				text := "finished"
				if len(in.Messages) > 0 && strings.Contains(in.Messages[0].Content, "Summarize the conversation") {
					mu.Lock()
					summaries++
					mu.Unlock()
					text = "CHILD-FACT preserved"
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", text)
			}))
			defer server.Close()
			t.Setenv("CHILD_FIT_KEY", "scripted-fixture")
			k, b, _ := dispatchKernel(t, &roleModel{})
			k.Budget = compact.Budget{Target: 100000, Ceiling: 200000}
			if err := config.Add(b.Root, config.Provider{Name: "small", Protocol: config.ChatCompletions, BaseURL: server.URL, APIKeyEnv: "CHILD_FIT_KEY"}, config.ModelSpec{Name: "small-child", Context: window}); err != nil {
				t.Fatal(err)
			}
			d := exec.Descriptor{Provider: "small", Model: "small-child"}
			endpoint, _, err := config.Resolve(b.Root, d.Provider, d.Model, config.Getenv)
			if err != nil {
				t.Fatal(err)
			}
			inner, err := provider.New(endpoint)
			if err != nil {
				t.Fatal(err)
			}
			wrapped := k.wrapSub(exec.StartRequest{Descriptor: &d}, inner, 4).(*fitProvider)
			req := provider.Request{Messages: []provider.Message{
				{Role: provider.RoleSystem, Content: provider.TextContent("child rules")},
				{Role: provider.RoleUser, Content: provider.TextContent("next task")},
			}}
			if scenario == "history" {
				req.Messages = []provider.Message{
					{Role: provider.RoleSystem, Content: provider.TextContent("child rules")},
					{Role: provider.RoleUser, Content: provider.TextContent(strings.Repeat("CHILD-FACT ", 6000))},
					{Role: provider.RoleAssistant, Content: provider.TextContent("prior result")},
					{Role: provider.RoleUser, Content: provider.TextContent("continue")},
				}
			}
			if scenario == "tool_schema" {
				req.Tools = []provider.ToolDef{{Name: "large", Description: strings.Repeat("schema ", 4000), Parameters: map[string]any{"type": "object"}}}
			}
			_, err = wrapped.Chat(context.Background(), req, nil)
			mu.Lock()
			n, s := calls, summaries
			mu.Unlock()
			switch scenario {
			case "tool_schema":
				if err == nil || n != 0 {
					t.Fatalf("oversized tool schema reached child: calls=%d err=%v", n, err)
				}
			case "history":
				if err != nil || s == 0 {
					t.Fatalf("history was not summarized by the child model: summaries=%d err=%v", s, err)
				}
			case "overflow":
				if err != nil || n-s != 2 || wrapped.fit.budget.ratio <= 1 {
					t.Fatalf("overflow did not calibrate/retry child: calls=%d ratio=%f err=%v", n, wrapped.fit.budget.ratio, err)
				}
			}
			if k.Budget.Ceiling != 200000 || k.scaled(100) != 100 {
				t.Fatal("child fitting changed parent window or calibration")
			}
		})
	}
}

func TestContinuableFirstTurnForkKeepsChildRulesAndOverlay(t *testing.T) {
	var mu sync.Mutex
	var received []provider.Message
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		mu.Lock()
		for _, m := range in.Messages {
			received = append(received, provider.Message{Role: m.Role, Content: provider.TextContent(m.Content)})
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	t.Setenv("EMPTY_FORK_KEY", "scripted-fixture")
	k, b, box := dispatchKernel(t, &roleModel{})
	for _, name := range []string{"parent", "child"} {
		if err := config.Add(b.Root, config.Provider{Name: name, Protocol: config.ChatCompletions, BaseURL: server.URL, APIKeyEnv: "EMPTY_FORK_KEY"}, config.ModelSpec{Name: name, Context: 30000}); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(b.Root, "prompts", "models", name)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(strings.ToUpper(name)+"-OVERLAY"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.SetBinding("parent", "parent"); err != nil {
		t.Fatal(err)
	}
	opening, err := b.AppendSeq(bot.Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("CURRENT-PARENT-SECRET")}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = k.StartSubagent(withTurnSeq(context.Background(), opening), SubagentRequest{Type: "research", Task: "child task", Context: "fork", Provider: "child", Model: "child"})
	if err != nil {
		t.Fatal(err)
	}
	waitEvent(t, box, exec.EvDone)
	mu.Lock()
	defer mu.Unlock()
	if len(received) < 2 || received[0].Role != provider.RoleSystem {
		t.Fatalf("lost child prefix: %+v", received)
	}
	text := ""
	for _, m := range received {
		text += m.Text() + "\n"
	}
	for _, want := range []string{exec.SubPersona, "You are a researcher.", "CHILD-OVERLAY", "child task"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in request", want)
		}
	}
	for _, banned := range []string{"PARENT-OVERLAY", "CURRENT-PARENT-SECRET"} {
		if strings.Contains(text, banned) {
			t.Fatalf("fork leaked %q", banned)
		}
	}
}
