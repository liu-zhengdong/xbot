package kernel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/config"
	"xbot/internal/link"
	"xbot/internal/provider"
	"xbot/internal/work"
)

// A Responses binding with hosted image generation on writes the vendor's
// picture into the bot workspace's generated/ directory. The conversation
// keeps the relative path, never the bytes, and nothing becomes a transcript
// image. Switching it off removes the tool on the next request without
// rebuilding the kernel; hosted search being off does not matter.
func TestHostedImageGenerationLandsInWorkspace(t *testing.T) {
	b := testBot(t, link.NameTools, link.NameTerminal)
	png := make([]byte, 4096)
	copy(png, "\x89PNG\r\n\x1a\n")
	b64 := base64.StdEncoding.EncodeToString(png)
	var mu sync.Mutex
	var tools [][]map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Tools []map[string]any `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		mu.Lock()
		tools = append(tools, req.Tools)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		item := `{"type":"image_generation_call","id":"ig_1","status":"completed","revised_prompt":"猫","result":"` + b64 + `"}`
		fmt.Fprint(w, `data: {"type":"response.output_item.done","output_index":0,"item":`+item+"}\n\n")
		fmt.Fprint(w, `data: {"type":"response.completed","response":{"status":"completed","output":[`+item+`,{"type":"message","id":"m1","content":[{"type":"output_text","text":"画好了"}]}]}}`+"\n\n")
	}))
	defer server.Close()
	off := false
	if err := config.Add(b.Root, config.Provider{Name: "openai", Protocol: config.Responses, BaseURL: server.URL, APIKeyEnv: "FAKE"}, config.ModelSpec{Name: "gpt-5.5", Context: 272000, HostedSearch: &off}); err != nil {
		t.Fatal(err)
	}
	if err := config.SetImageGen(b.Root, "openai", "gpt-5.5", true, "gpt-image-2.5-flare"); err != nil {
		t.Fatal(err)
	}
	if err := b.SetBinding("openai", "gpt-5.5"); err != nil {
		t.Fatal(err)
	}
	ep, _, err := config.Resolve(b.Root, "openai", "gpt-5.5", func(string) string { return "fake" })
	if err != nil {
		t.Fatal(err)
	}
	p, err := provider.New(ep)
	if err != nil {
		t.Fatal(err)
	}
	k := &Kernel{Bot: b, Links: []link.Link{bindingSearchLink{}}, Provider: p, MaxSteps: 2, Now: func() time.Time { return time.Unix(1000, 0) }}
	var progress []string
	k.OnPart = func(p provider.ContentPart) {
		if p.Hosted != nil && p.Hosted.Name == "image_generation" {
			progress = append(progress, p.Text)
		}
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("owner", "画只猫", time.Unix(1000, 0))); err != nil {
		t.Fatal(err)
	}
	gen := filepath.Join(work.BotDir(b.Root, b.Name()), work.GeneratedDir)
	files, err := os.ReadDir(gen)
	if err != nil || len(files) != 1 {
		t.Fatalf("generated files %v %v", files, err)
	}
	rel := work.GeneratedDir + "/" + files[0].Name()
	got, err := os.ReadFile(filepath.Join(gen, files[0].Name()))
	if err != nil || string(got) != string(png) {
		t.Fatalf("file content %d %v", len(got), err)
	}
	if _, err := os.Stat(filepath.Join(work.BotDir(b.Root, b.Name()), work.PastedDir)); err == nil {
		t.Fatal("generated picture went to paste/")
	}
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		raw, _ := json.Marshal(e)
		if strings.Contains(string(raw), b64[:64]) {
			t.Fatal("base64 in transcript")
		}
		if len(e.Images) > 0 {
			t.Fatalf("generated picture became a transcript image: %v", e.Images)
		}
		for _, part := range e.Message.Content {
			if part.Hosted != nil && part.Hosted.Name == "image_generation" && strings.Contains(part.Text, rel) {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("transcript lacks the generated path")
	}
	if len(progress) == 0 || !strings.Contains(progress[len(progress)-1], rel) {
		t.Fatalf("progress %v", progress)
	}
	mu.Lock()
	first := tools[0]
	mu.Unlock()
	var img map[string]any
	for _, tool := range first {
		if tool["type"] == "web_search" {
			t.Fatal("search was off")
		}
		if tool["type"] == "image_generation" {
			img = tool
		}
	}
	if img == nil || img["model"] != "gpt-image-2.5-flare" {
		t.Fatalf("image tool %v", first)
	}

	if err := config.SetImageGen(b.Root, "openai", "gpt-5.5", false, ""); err != nil {
		t.Fatal(err)
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("owner", "再来", time.Unix(1001, 0))); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	last := tools[len(tools)-1]
	mu.Unlock()
	for _, tool := range last {
		if tool["type"] == "image_generation" {
			t.Fatal("image generation still mounted after switching off")
		}
	}
}
