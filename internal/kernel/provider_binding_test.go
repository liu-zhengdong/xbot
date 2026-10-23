package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"xbot/internal/config"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/tools"
)

type bindingSearchTool string

func (t bindingSearchTool) Name() string                                       { return string(t) }
func (bindingSearchTool) Confirm(json.RawMessage) (bool, string, error)        { return false, "", nil }
func (bindingSearchTool) Description() string                                  { return "local search fixture" }
func (bindingSearchTool) Parameters() any                                      { return schema(nil, nil) }
func (bindingSearchTool) Run(context.Context, json.RawMessage) (string, error) { return "local", nil }

type bindingSearchLink struct{}

func (bindingSearchLink) Name() string { return link.NameTools }
func (bindingSearchLink) Tools() []tools.Tool {
	return []tools.Tool{bindingSearchTool("web_search"), bindingSearchTool("web_fetch")}
}

func TestBindingSearchExclusionReplayEnvironmentAndJobs(t *testing.T) {
	b := testBot(t, link.NameTools, link.NameTerminal)
	var requests []struct {
		Model string `json:"model"`
		Tools []struct {
			Type     string `json:"type"`
			Name     string `json:"name"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
		Input json.RawMessage `json:"input"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
			Tools []struct {
				Type     string `json:"type"`
				Name     string `json:"name"`
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
			Input json.RawMessage `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		requests = append(requests, req)
		w.Header().Set("Content-Type", "text/event-stream")
		if r.URL.Path == "/responses" {
			fmt.Fprint(w, "data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"web_search_call\",\"id\":\"ws_test\",\"status\":\"in_progress\"}}\n\n")
			fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"web_search_call\",\"id\":\"ws_test\",\"status\":\"completed\",\"action\":{\"query\":\"news\",\"type\":\"search\"}},{\"type\":\"message\",\"id\":\"msg_test\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"消息已找到\",\"annotations\":[{\"type\":\"url_citation\",\"url\":\"https://example.org\",\"title\":\"来源\",\"start_index\":0,\"end_index\":5}]}]}],\"usage\":{\"input_tokens\":100,\"output_tokens\":10}}}\n\n")
		} else {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"本地绑定\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		}
	}))
	defer server.Close()
	for _, p := range []config.Provider{{Name: "deepseek", Protocol: config.ChatCompletions, BaseURL: server.URL, APIKeyEnv: "FAKE"}, {Name: "openai", Protocol: config.Responses, BaseURL: server.URL, APIKeyEnv: "FAKE"}} {
		m := config.ModelSpec{Name: "deepseek-flash", Context: 1000000}
		if p.Name == "openai" {
			m.Name = "gpt-5.5"
			m.Context = 272000
		}
		if err := config.Add(b.Root, p, m); err != nil {
			t.Fatal(err)
		}
	}
	k := &Kernel{Bot: b, Links: []link.Link{bindingSearchLink{}}, MaxSteps: 2, Now: func() time.Time { return time.Unix(1000, 0) }}
	bind := func(name, model string) {
		t.Helper()
		if err := b.SetBinding(name, model); err != nil {
			t.Fatal(err)
		}
		ep, _, err := config.Resolve(b.Root, name, model, func(string) string { return "fake" })
		if err != nil {
			t.Fatal(err)
		}
		k.Provider, err = provider.New(ep)
		if err != nil {
			t.Fatal(err)
		}
	}
	run := func() {
		t.Helper()
		if err := k.Handle(context.Background(), link.TerminalEvent("owner", "search", time.Unix(1000, 0))); err != nil {
			t.Fatal(err)
		}
	}
	bind("deepseek", "deepseek-flash")
	run()
	if !slices.Contains(environmentGet[[]string](testPublished(t, k), "tools"), "web_search") {
		t.Fatal("local search missing")
	}
	bind("openai", "gpt-5.5")
	progress := 0
	k.OnPart = func(p provider.ContentPart) {
		if p.Hosted != nil {
			progress++
		}
	}
	run()
	names := environmentGet[[]string](testPublished(t, k), "tools")
	if slices.Contains(names, "web_search") || !slices.Contains(names, "hosted:web_search") || !slices.Contains(names, "web_fetch") {
		t.Fatalf("bad hosted environment %v", names)
	}
	if k.contextWindow() != 272000 || progress != 1 {
		t.Fatalf("window %d progress %d", k.contextWindow(), progress)
	}
	next, err := k.sampleEnvironment(testPublished(t, k))
	if err != nil {
		t.Fatal(err)
	}
	if !reflectToolsEqual(testPublished(t, k), next) {
		t.Fatal("same binding repeated tools update")
	}
	run()
	if !strings.Contains(string(requests[2].Input), `"web_search_call"`) {
		t.Fatal("native hosted history not replayed")
	}
	for _, req := range requests[1:3] {
		hosted := 0
		for _, tool := range req.Tools {
			if tool.Function.Name == "web_search" || tool.Name == "web_search" {
				t.Fatal("hosted binding has local search")
			}
			if tool.Type == "web_search" {
				hosted++
			}
		}
		if hosted != 1 {
			t.Fatal("missing hosted schema")
		}
	}
	if len(k.shellJobs().List()) != 0 {
		t.Fatal("hosted entered shell Jobs")
	}
	metas, _ := filepath.Glob(filepath.Join(b.AgentDir("*"), "meta.json"))
	if len(metas) != 0 {
		t.Fatalf("hosted entered subagent Jobs: %v", metas)
	}
	traces, err := b.Traces()
	if err != nil {
		t.Fatal(err)
	}
	billed := 0
	for _, tr := range traces {
		billed += tr.HostedCalls
	}
	if billed != 2 {
		t.Fatalf("hosted billing count %d", billed)
	}
	bind("deepseek", "deepseek-flash")
	run()
	if !slices.Contains(environmentGet[[]string](testPublished(t, k), "tools"), "web_search") || k.contextWindow() != 1000000 {
		t.Fatal("Completions local search/window not restored")
	}
	local := false
	for _, tool := range requests[3].Tools {
		if tool.Function.Name == "web_search" {
			local = true
		}
		if tool.Type == "web_search" {
			t.Fatal("hosted leaked into Completions")
		}
	}
	if !local {
		t.Fatal("local schema missing after switch")
	}
}
func reflectToolsEqual(a, b environment) bool {
	return slices.Equal(environmentGet[[]string](a, "tools"), environmentGet[[]string](b, "tools"))
}

func TestHostedSearchScopedTools(t *testing.T) {
	b := testBot(t, link.NameTools)
	k := &Kernel{Bot: b, WorkDir: t.TempDir(), Provider: &provider.Responses{WebSearch: true}}
	for label, list := range map[string][]tools.Tool{"scoped": k.toolsForScope("scope-test"), "subagent": k.subagentBase("scope-test")} {
		names := toolNames(list)
		if slices.Contains(names, "web_search") || !slices.Contains(names, "web_fetch") {
			t.Fatalf("%s search set %v", label, names)
		}
	}
}

func TestHostedSearchToolFreeSummary(t *testing.T) {
	b := testBot(t, link.NameTools)
	k := &Kernel{Bot: b, Provider: &provider.Responses{WebSearch: true}}
	k.AllowFork = true
	r := &turnRun{k: k, p: k.Provider}
	req, err := r.requestContext(provider.Request{})
	if err != nil || req.HostedSearch {
		t.Fatalf("summary opened hosted search: %+v %v", req, err)
	}
	req, err = r.requestContext(provider.Request{Tools: []provider.ToolDef{}})
	if err != nil || !req.HostedSearch {
		t.Fatalf("normal tool round missing hosted search: %+v %v", req, err)
	}
}

func TestBindingIdleRejectsRunningToolJob(t *testing.T) {
	b := testBot(t)
	k := &Kernel{Bot: b}
	jobs := k.shellJobs()
	jobs.OnDone = nil
	defer jobs.Shutdown()
	started := make(chan struct{})
	jobs.RunCall(context.Background(), tools.CallRequest{Name: "web_fetch", Background: true, Run: func(ctx context.Context) tools.CallResult { close(started); <-ctx.Done(); return tools.CallResult{} }})
	<-started
	if err := k.CheckBindingIdle(); err == nil {
		t.Fatal("running tool Job would lose its cancel handle")
	}
}

func TestBindingToolsBudgetBeforeFirstFitWithoutHostedSearch(t *testing.T) {
	b := testBot(t)
	k := &Kernel{Bot: b, AllowFork: true, Provider: &provider.Client{}}
	r := &turnRun{k: k, p: k.Provider}
	_, err := r.requestContext(provider.Request{Tools: []provider.ToolDef{{Name: "fake", Description: "definition must be reserved before fit"}}})
	if err != nil {
		t.Fatal(err)
	}
	if k.context.tools == 0 {
		t.Fatal("first fit would use a stale tool budget")
	}
}
