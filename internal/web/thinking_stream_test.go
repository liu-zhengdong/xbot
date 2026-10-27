package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

// With thinking on, a DeepSeek tool turn needs its reasoning_content back on
// the next request, and the reasoning stays out of everything the owner sees.
func TestThinkingToolTurnReplaysReasoningAndHidesIt(t *testing.T) {
	var mu sync.Mutex
	var calls int
	var thinking []string
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Thinking *struct {
				Type string `json:"type"`
			} `json:"thinking"`
			Messages []map[string]any `json:"messages"`
		}
		_ = json.Unmarshal(raw, &req)
		mu.Lock()
		calls++
		n := calls
		if req.Thinking != nil {
			thinking = append(thinking, req.Thinking.Type)
		}
		mu.Unlock()
		if n > 1 {
			// DeepSeek: an assistant tool turn without its reasoning is a 400.
			ok := false
			for _, m := range req.Messages {
				if m["role"] == "assistant" && m["tool_calls"] != nil {
					ok = m["reasoning_content"] == "SECRET-THOUGHT-1"
					if strings.Contains(fmt.Sprint(m["content"]), "SECRET-THOUGHT") {
						ok = false
					}
				}
			}
			if !ok {
				http.Error(w, `{"error":{"message":"Missing reasoning_content field"}}`, http.StatusBadRequest)
				return
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"SECRET-THOUGHT-1\"}}]}\n\n")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"no_such_tool\",\"arguments\":\"{}\"}}]}}]}\n\n")
		} else {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"SECRET-THOUGHT-2\"}}]}\n\n")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"完成。\"}}]}\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer model.Close()
	s := &Server{Root: t.TempDir(), Owner: "u1"}
	tok, _, err := s.EnsureToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(s.Root, "ada", "persona", []string{link.NameWeb})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	s.NewKernel = func(b *bot.Bot) (*kernel.Kernel, error) {
		return &kernel.Kernel{Bot: b, Provider: &provider.Client{BaseURL: model.URL, APIKey: "fake", Model: "deepseek-flash"}, Memory: memory.NopStore{}, MaxSteps: 4}, nil
	}
	ui := httptest.NewServer(muxOf(s))
	defer ui.Close()
	cookie := loginCookie(t, ui.URL, tok)
	res := postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, `{"text":"查一下"}`)
	stream := res.Body
	if !strings.Contains(string(stream), "event: done") {
		t.Fatalf("second tool turn failed: %s", stream)
	}
	if strings.Contains(string(stream), "SECRET-THOUGHT") {
		t.Fatalf("reasoning reached the live stream: %s", stream)
	}
	mu.Lock()
	if calls != 2 || len(thinking) != 2 || thinking[0] != "enabled" || thinking[1] != "enabled" {
		t.Fatalf("calls=%d thinking=%v", calls, thinking)
	}
	mu.Unlock()
	history := getAuth(t, ui.URL+"/api/bots/ada/transcript", cookie)
	// Readable thinking goes only to the trace's thinking field, never as content or vendor raw.
	if !strings.Contains(string(history), `"thinking":"SECRET-THOUGHT-1"`) || strings.Count(string(history), "SECRET-THOUGHT") != 2 ||
		strings.Contains(string(history), "vendor_raw") || strings.Contains(string(history), "reasoning_content") || !strings.Contains(string(history), "完成。") {
		t.Fatalf("transcript API: %s", history)
	}
	// The internal transcript keeps it for replay.
	var found bool
	_ = filepath.Walk(s.Root, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(path, ".jsonl") {
			data, _ := os.ReadFile(path)
			found = found || strings.Contains(string(data), "SECRET-THOUGHT-1")
		}
		return nil
	})
	if !found {
		t.Fatal("reasoning not kept in the internal transcript")
	}
}
