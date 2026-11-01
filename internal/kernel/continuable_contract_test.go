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

	"xbot/internal/compact"
	"xbot/internal/config"
	"xbot/internal/exec"
	"xbot/internal/provider"
)

// Messages are under the original soft target, but definitions leave a smaller
// message budget. This must summarize rather than skip fit and reject the chat.
func TestContinuableFitsBelowSoftTargetWhenToolsConsumeWindow(t *testing.T) {
	model := &roleModel{}
	k, _, _ := dispatchKernel(t, model)
	summary := &recordCompactor{}
	f := newTurnFit(1, nil)
	f.budget = &subagentBudget{target: 120, ceiling: 400, ratio: 1, compactor: summary}
	p := &fitProvider{k: k, inner: model, fit: f}
	req := provider.Request{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: provider.TextContent("child rules")},
		{Role: provider.RoleUser, Content: provider.TextContent(strings.Repeat("a", 320))},
		{Role: provider.RoleAssistant, Content: provider.TextContent("prior result")},
		{Role: provider.RoleUser, Content: provider.TextContent("next")},
	}, Tools: []provider.ToolDef{{Name: "t", Description: strings.Repeat("s", 1200), Parameters: map[string]any{"type": "object"}}}}
	size := compact.EstimateMessages(req.Messages)
	if size > f.budget.target || size+estimateTools(req.Tools) <= f.budget.ceiling {
		t.Fatalf("fixture misses target/hard boundary: messages=%d tools=%d", size, estimateTools(req.Tools))
	}
	if _, err := p.Chat(context.Background(), req, nil); err != nil {
		t.Fatal(err)
	}
	if len(summary.last) == 0 {
		t.Fatal("did not summarize the reducible history")
	}
	model.mu.Lock()
	defer model.mu.Unlock()
	if len(model.foreReq) != 1 {
		t.Fatalf("requests = %d", len(model.foreReq))
	}
	sent := model.foreReq[0]
	if compact.EstimateMessages(sent.Messages)+estimateTools(sent.Tools) > f.budget.ceiling {
		t.Fatal("sent over the hard ceiling")
	}
}

func TestContinuableMCPSchemaFallbackIsolatesActualRoutes(t *testing.T) {
	var mu sync.Mutex
	received := map[string][][]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Model string `json:"model"`
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		names := []string{}
		bad := false
		for _, tool := range in.Tools {
			names = append(names, tool.Function.Name)
			if tool.Function.Name == "mcp_schema_bad" {
				bad = true
			}
		}
		mu.Lock()
		received[in.Model] = append(received[in.Model], names)
		mu.Unlock()
		if in.Model == "route-b" && bad {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"message":"Invalid schema for function 'mcp_schema_bad'"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	t.Setenv("CHILD_SCHEMA_KEY", "scripted-fixture")
	k, b, _ := dispatchKernel(t, &roleModel{})
	for _, model := range []string{"route-a", "route-b"} {
		if err := config.Add(b.Root, config.Provider{Name: "fixture", Protocol: config.ChatCompletions, BaseURL: server.URL, APIKeyEnv: "CHILD_SCHEMA_KEY"}, config.ModelSpec{Name: model, Context: 30000}); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.SetBinding("fixture", "route-a"); err != nil {
		t.Fatal(err)
	}
	req := schemaRequest()
	req.Messages = append([]provider.Message{{Role: provider.RoleSystem, Content: provider.TextContent("child rules")}}, req.Messages...)
	for _, model := range []string{"route-b", "route-a", "route-b"} {
		ep, _, err := config.Resolve(b.Root, "fixture", model, config.Getenv)
		if err != nil {
			t.Fatal(err)
		}
		p, err := provider.New(ep)
		if err != nil {
			t.Fatal(err)
		}
		child := k.wrapSub(exec.StartRequest{Descriptor: &exec.Descriptor{Provider: "fixture", Model: model}}, p, len(req.Messages))
		if _, err := child.Chat(context.Background(), req, nil); err != nil {
			t.Fatal(err)
		}
	}
	// The same Bot's parent route A must retain its original schema too.
	if filtered := (&mcpSchemaAttempt{k: k}).filter(req); len(filtered.Tools) != len(req.Tools) {
		t.Fatal("route B poisoned the parent's route A cache")
	}
	mu.Lock()
	defer mu.Unlock()
	a, bCalls := received["route-a"], received["route-b"]
	if len(a) != 1 || len(a[0]) != 3 {
		t.Fatalf("route A lost its schema: %v", a)
	}
	if len(bCalls) != 3 || len(bCalls[0]) != 3 || len(bCalls[1]) != 2 || len(bCalls[2]) != 2 {
		t.Fatalf("route B did not retain its own fallback: %v", bCalls)
	}
}
