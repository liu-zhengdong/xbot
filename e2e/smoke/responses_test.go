//go:build smoke

package smoke

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"xbot/internal/config"
)

// Real serve, binding HTTP API, native protocol and transcript. Both models
// are scripted; no model keys or provider network calls are used.
func TestResponsesBindingSearch(t *testing.T) {
	stub := StartStub(t)
	srv := Start(t, stub)
	srv.CreateBot("ada", "Be brief.")
	var mu sync.Mutex
	calls := 0
	var failure string
	responses := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Tools []struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"tools"`
			Input json.RawMessage `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		mu.Lock()
		calls++
		if r.URL.Path != "/responses" {
			failure = "wrong endpoint"
		}
		hosted := 0
		for _, tool := range req.Tools {
			if tool.Name == "web_search" {
				failure = "local search offered to GPT"
			}
			if tool.Type == "web_search" {
				hosted++
			}
		}
		if hosted != 1 {
			failure = "hosted search missing"
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"web_search_call\",\"id\":\"ws_smoke\",\"status\":\"in_progress\"}}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"搜索结果带引用\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"web_search_call\",\"id\":\"ws_smoke\",\"status\":\"completed\",\"action\":{\"type\":\"search\",\"query\":\"新闻\"}},{\"type\":\"message\",\"id\":\"msg_smoke\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"搜索结果带引用\",\"annotations\":[{\"type\":\"url_citation\",\"url\":\"https://example.org/source\",\"title\":\"新闻来源\",\"start_index\":0,\"end_index\":7}]}]}],\"usage\":{\"input_tokens\":100,\"output_tokens\":10}}}\n\n")
	}))
	defer responses.Close()
	if err := config.Add(srv.Root, config.Provider{Name: "openai", Protocol: config.Responses, BaseURL: responses.URL, APIKeyEnv: "XBOT_API_KEY"}, config.ModelSpec{Name: "gpt-5.5", Context: 1000000}); err != nil {
		t.Fatal(err)
	}
	if status, body := srv.do("PUT", "/api/bots/ada/binding", map[string]string{"provider": "openai", "model": "gpt-5.5"}); status != 200 {
		t.Fatalf("bind %d %s", status, body)
	}
	result := srv.Send("ada", "查一下新闻")
	if result.Done != "搜索结果带引用" || len(result.Tools) != 0 || result.Error != "" {
		t.Fatalf("hosted SSE %+v", result)
	}
	_, raw := srv.do("GET", "/api/bots/ada/transcript", nil)
	if !strings.Contains(string(raw), `"server_tool"`) || !strings.Contains(string(raw), `"citation"`) || strings.Contains(string(raw), "vendor_raw") {
		t.Fatalf("public blocks %s", raw)
	}
	trace, err := os.ReadFile(filepath.Join(srv.Root, "bots", "ada", "trace.jsonl"))
	if err != nil || !strings.Contains(string(trace), `"hosted_calls":1`) {
		t.Fatalf("hosted cost missing %v %s", err, trace)
	}
	meta, _ := filepath.Glob(filepath.Join(srv.Root, "bots", "ada", "agents", "*", "meta.json"))
	if len(meta) != 0 {
		t.Fatalf("hosted Jobs %v", meta)
	}
	if status, body := srv.do("PUT", "/api/bots/ada/binding", map[string]string{"provider": "", "model": ""}); status != 200 {
		t.Fatalf("restore %d %s", status, body)
	}
	reply := srv.Send("ada", "SMOKE_HELLO_1")
	checkStreamed(t, "Completions restored", reply, "你好，这是第 1 次流式回复")
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 || failure != "" {
		t.Fatalf("Responses calls %d: %s", calls, failure)
	}
}
