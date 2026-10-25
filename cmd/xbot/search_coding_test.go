package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/code"
	"xbot/internal/config"
	"xbot/internal/inbox"
	"xbot/internal/link"
)

func TestOpenedKernelCodingUsesSearchSettings(t *testing.T) {
	root := t.TempDir()
	defer config.ForgetKeys()
	t.Setenv("FAKE_SEARCH_CODING_KEY", "fake-search-coding-key")
	requests := make(chan bool, 3)
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
	if err := config.Add(root, config.Provider{Name: "fake", Protocol: config.Responses, BaseURL: server.URL, APIKeyEnv: "FAKE_SEARCH_CODING_KEY"}, config.ModelSpec{Name: "gpt-5.5", Context: 128000}); err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "coding-search", "persona", []string{link.NameCoding})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetBinding("fake", "gpt-5.5"); err != nil {
		t.Fatal(err)
	}
	k, err := newServeKernel(root, b)
	if err != nil {
		t.Fatal(err)
	}
	var coding *code.Link
	for _, l := range k.Links {
		if c, ok := l.(*code.Link); ok {
			coding = c
		}
	}
	if coding == nil {
		t.Fatal("coding link missing")
	}
	coding.UseCommand("")
	for i, step := range []struct {
		enabled bool
		mode    string
		want    bool
	}{{false, "auto", false}, {true, "local", false}, {true, "auto", true}} {
		if err := config.SetWebSearch(root, step.enabled, step.mode, "ddg"); err != nil {
			t.Fatal(err)
		}
		args, _ := json.Marshal(map[string]any{"work_dir": t.TempDir(), "max_steps": 1, "timeout_sec": 3, "goal": "reply done"})
		if _, err := coding.Tools()[0].Run(context.Background(), args); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-requests:
			if got != step.want {
				t.Fatalf("opened coding hosted=%t want=%t", got, step.want)
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
