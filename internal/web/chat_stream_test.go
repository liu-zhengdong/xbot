package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/provider"
)

func TestResponsesStreamPersistence(t *testing.T) {
	for _, mode := range []string{"items", "deltas", "empty"} {
		t.Run(mode, func(t *testing.T) {
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				if mode != "empty" {
					fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"我是助手。\"}\n\n")
				}
				if mode == "items" {
					fmt.Fprint(w, "data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"我是助手。\"}]}}\n\n")
				}
				fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n")
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
				return &kernel.Kernel{Bot: b, Provider: &provider.Responses{Client: provider.Client{BaseURL: model.URL, APIKey: "fake", Model: "fake"}}, Memory: memory.NopStore{}, MaxSteps: 2}, nil
			}
			ui := httptest.NewServer(muxOf(s))
			defer ui.Close()
			cookie := loginCookie(t, ui.URL, tok)
			res := postJSON(t, ui.URL+"/api/bots/ada/messages", cookie, `{"text":"你现在是什么模型？"}`)
			events := parseSSE(t, res.Body)
			if mode == "empty" {
				if !strings.Contains(events["error"], "empty response") {
					t.Fatalf("missing empty reply error: %s", res.Body)
				}
			} else if !strings.Contains(events["delta"], "我是助手。") || !strings.Contains(events["done"], "我是助手。") {
				t.Fatalf("stream lost reply: %s", res.Body)
			}
			var transcript struct {
				Messages []apiRow `json:"messages"`
			}
			if err := json.Unmarshal(getAuth(t, ui.URL+"/api/bots/ada/transcript", cookie), &transcript); err != nil {
				t.Fatal(err)
			}
			users, assistants := 0, 0
			for _, row := range transcript.Messages {
				if row.Role == provider.RoleUser && row.Content == "你现在是什么模型？" {
					users++
				}
				if row.Role == provider.RoleAssistant {
					assistants++
					if mode == "empty" && !strings.Contains(row.Content, "这一轮出错") {
						t.Fatalf("empty response has no persisted failure notice: %+v", row)
					}
					if mode != "empty" && row.Content != "我是助手。" {
						t.Fatalf("empty or changed persisted answer: %+v", row)
					}
				}
			}
			if users != 1 || assistants != 1 {
				t.Fatalf("users=%d assistants=%d", users, assistants)
			}
		})
	}
}
