package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"xbot/internal/code"

	"xbot/internal/config"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/provider"
)

func TestGlobalSearchPolicyUpdatesExistingKernelAndExecutor(t *testing.T) {
	b := testBot(t, link.NameTools)
	k := &Kernel{Bot: b, Links: []link.Link{bindingSearchLink{}}, Provider: &provider.Responses{WebSearch: true}}
	// This provider is cached by Local exactly like ensureExec; no reconstruction.
	cached := k.boundProvider()
	for _, step := range []struct {
		enabled       bool
		mode          string
		local, hosted bool
	}{
		{true, "auto", false, true}, {false, "auto", false, false}, {true, "local", true, false}, {true, "auto", false, true},
	} {
		if err := config.SetWebSearch(b.Root, step.enabled, step.mode, "ddg"); err != nil {
			t.Fatal(err)
		}
		list, err := k.mountedTools()
		if err != nil {
			t.Fatal(err)
		}
		local := false
		for _, tool := range list {
			local = local || tool.Name() == "web_search"
		}
		if local != step.local || provider.HasHostedSearch(cached) != step.hosted {
			t.Fatalf("mode=%s on=%t local=%t hosted=%t", step.mode, step.enabled, local, provider.HasHostedSearch(cached))
		}
		scoped := k.toolsForScope("test")
		local = false
		for _, tool := range scoped {
			local = local || tool.Name() == "web_search"
		}
		if local != step.local {
			t.Fatal("scoped policy differs")
		}
		ep, err := k.endpoint()
		if err != nil || ep.HostedSearch != step.hosted {
			t.Fatalf("remote endpoint: %+v %v", ep, err)
		}
	}
	k.Provider = &provider.Client{}
	list, err := k.mountedTools()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range list {
		found = found || tool.Name() == "web_search"
	}
	if !found {
		t.Fatal("automatic mode did not restore local search")
	}
}

func TestConfiguredSearchReadsNewSelectionAndOff(t *testing.T) {
	b := testBot(t, link.NameTools)
	// Even an old tool reference must not call the network after switching off.
	search := configuredSearch{root: b.Root}
	if err := config.SetWebSearch(b.Root, false, "auto", "ddg"); err != nil {
		t.Fatal(err)
	}
	if _, err := search.Search(context.Background(), "query", 1); err == nil || !strings.Contains(err.Error(), "关闭") {
		t.Fatal(err)
	}
	if err := config.SetWebSearch(b.Root, true, "local", "brave"); err != nil {
		t.Fatal(err)
	}
	if _, err := search.Search(context.Background(), "query", 1); err == nil || !strings.Contains(err.Error(), "暂时") {
		t.Fatal(err)
	}
	if err := config.SetWebSearch(b.Root, true, "local", "deepseek"); err != nil {
		t.Fatal(err)
	}
	if _, err := search.Search(context.Background(), "query", 1); err == nil || !strings.Contains(err.Error(), "密钥") {
		t.Fatal(err)
	}
}

type searchCapture struct{ req provider.Request }

func (p *searchCapture) Chat(_ context.Context, r provider.Request, _ func(string)) (provider.Message, error) {
	p.req = r
	return provider.Message{}, nil
}
func TestSearchPolicyFiltersStaleExecutorTools(t *testing.T) {
	b := testBot(t, link.NameTools)
	p := &searchCapture{}
	k := &Kernel{Bot: b, Provider: p}
	cached := k.boundProvider()
	if err := config.SetWebSearch(b.Root, false, "auto", "ddg"); err != nil {
		t.Fatal(err)
	}
	req := provider.Request{Tools: []provider.ToolDef{{Name: "web_search"}, {Name: "web_fetch"}}}
	if _, err := cached.Chat(context.Background(), req, nil); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(p.req.Tools)
	if strings.Contains(string(raw), "web_search") || len(p.req.Tools) != 1 {
		t.Fatal(string(raw))
	}
	if len(req.Tools) != 2 || req.Tools[0].Name != "web_search" {
		t.Fatal("caller mutated")
	}
}

func TestNewlyMountedCodingUsesDynamicSearchPolicy(t *testing.T) {
	b := testBot(t, link.NameCoding)
	requests := make(chan bool, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Tools []struct{ Type string } }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		hosted := false
		for _, tool := range in.Tools {
			hosted = hosted || tool.Type == "web_search"
		}
		requests <- hosted
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"done\"}]}]}}\n\n")
	}))
	defer server.Close()
	p, err := provider.New(provider.Endpoint{Protocol: provider.ResponsesProtocol, BaseURL: server.URL, Model: "fake", APIKey: "fake", HostedSearch: true})
	if err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, Provider: p}
	coding := k.mountGrantLocked(link.NameCoding).(*code.Link)
	coding.UseCommand("")
	for i, step := range []struct {
		enabled bool
		mode    string
		want    bool
	}{{false, "auto", false}, {true, "local", false}, {true, "auto", true}} {
		if err := config.SetWebSearch(b.Root, step.enabled, step.mode, "ddg"); err != nil {
			t.Fatal(err)
		}
		args, _ := json.Marshal(map[string]any{"work_dir": t.TempDir(), "max_steps": 1, "timeout_sec": 3, "goal": "reply done"})
		if _, err := coding.Tools()[0].Run(context.Background(), args); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-requests:
			if got != step.want {
				t.Fatalf("coding hosted=%t want=%t", got, step.want)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("coding request missing")
		}
		deadline := time.Now().Add(3 * time.Second)
		for {
			box, err := inbox.Open(b.InboxPath())
			if err == nil && len(box.Pending()) >= i+1 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("coding completion missing")
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}
