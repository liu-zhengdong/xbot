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
	"xbot/internal/vault"
	"xbot/internal/work"
)

// A Responses binding with hosted image generation on writes the vendor's
// picture into the bot workspace's generated/ directory. The conversation
// keeps the relative path, never the bytes. Saving alone does not attach a
// picture. Switching it off removes the tool on the next request without
// rebuilding the kernel; hosted search being off does not matter.
func TestHostedImageGenerationLandsInWorkspace(t *testing.T) {
	b := testBot(t, link.NameTools, link.NameTerminal)
	png := sizedPNG(t, 16, 16)
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
	if err := config.Add(b.Root, config.Provider{Name: "openai", Protocol: config.Responses, BaseURL: server.URL, OAuth: "chatgpt"}, config.ModelSpec{Name: "gpt-5.5", Context: 272000, HostedSearch: &off}); err != nil {
		t.Fatal(err)
	}
	if err := config.SetImageGen(b.Root, "openai", "gpt-5.5", true, "gpt-image-2.5-flare"); err != nil {
		t.Fatal(err)
	}
	v, err := config.OAuthVault(b.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.SetOAuth(v, "chatgpt", vault.OAuthCredential{AccessToken: "fake"}, []string{"127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	if err := config.SetWebImageGen(b.Root, true, "openai"); err != nil {
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
	k.WorkDir = work.BotDir(b.Root, b.Name())
	if err := os.MkdirAll(filepath.Join(k.WorkDir, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	k.storeCwd(filepath.Join(k.WorkDir, "nested"))
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
	id, _ := work.PastedNameFor("image/png", png)
	if _, err := work.ReadPasted(k.WorkDir, id); err == nil {
		t.Fatal("Save copied image into paste without notify_owner")
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
			t.Fatalf("Save attached image without notify_owner: %v", e.Images)
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
	if len(progress) == 0 || progress[len(progress)-1] != "[厂商生图: 已保存到工作区 "+rel+"]" {
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

	if err := config.SetWebImageGen(b.Root, false, "openai"); err != nil {
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

func TestImageGenGlobalGate(t *testing.T) {
	for _, tc := range []struct {
		name, selected, oauth         string
		enabled, logged, legacy, want bool
	}{
		{"default off", "openai", "chatgpt", false, true, true, false},
		{"openai oauth", "openai", "chatgpt", true, true, false, true},
		{"xai oauth", "xai", "xai", true, true, false, true},
		{"wrong selection", "xai", "chatgpt", true, true, true, false},
		{"not logged in", "openai", "chatgpt", true, false, true, false},
		{"api key only", "openai", "", true, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := testBot(t, link.NameTools, link.NameTerminal)
			keyEnv := ""
			if tc.oauth == "" {
				keyEnv = "FAKE"
			}
			if err := config.Add(b.Root, config.Provider{Name: "custom-name", Protocol: config.Responses, BaseURL: "http://127.0.0.1:1", OAuth: tc.oauth, APIKeyEnv: keyEnv}, config.ModelSpec{Name: "model", Context: 1000, HostedImageGen: &tc.legacy}); err != nil {
				t.Fatal(err)
			}
			if err := b.SetBinding("custom-name", "model"); err != nil {
				t.Fatal(err)
			}
			if tc.enabled {
				if err := config.SetWebImageGen(b.Root, true, tc.selected); err != nil {
					t.Fatal(err)
				}
			}
			if tc.logged {
				v, err := config.OAuthVault(b.Root)
				if err != nil {
					t.Fatal(err)
				}
				kind := tc.oauth
				if kind == "" {
					kind = "chatgpt"
				}
				if err := vault.SetOAuth(v, kind, vault.OAuthCredential{AccessToken: "fake"}, []string{"127.0.0.1"}); err != nil {
					t.Fatal(err)
				}
			}
			k := &Kernel{Bot: b}
			inherited := provider.WithImageGen(context.Background(), provider.ImageGen{Save: func([]byte) (string, error) { return "", nil }})
			ctx := k.withImageGen(context.Background())
			if got := provider.HasImageGen(ctx); got != tc.want {
				t.Fatalf("mounted=%v want=%v", got, tc.want)
			}
			if got := provider.HasImageGen(k.withImageGen(inherited)); got != tc.want {
				t.Fatalf("inherited mounted=%v want=%v", got, tc.want)
			}
			if tc.want {
				if err := config.SetWebImageGen(b.Root, false, tc.selected); err != nil {
					t.Fatal(err)
				}
				r := &turnRun{k: k, t: &turn{runCtx: ctx}}
				if !provider.HasImageGen(r.modelContext()) {
					t.Fatal("running turn lost its snapshot")
				}
				if provider.HasImageGen(k.withImageGen(context.Background())) {
					t.Fatal("next turn kept disabled capability")
				}
			}
		})
	}
}

// Hosted Save only stores files. Delivery limits and deduplication apply when
// the Bot explicitly calls notify_owner with the receipt paths.
func TestHostedImageDeliveryLimitsAndIdempotency(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		pics                  func(*testing.T) [][]byte
		wantImages, wantFiles int
		failure               string
	}{
		{"duplicate content", func(t *testing.T) [][]byte { p := sizedPNG(t, 1, 1); return [][]byte{p, p} }, 1, 2, ""},
		{"count cap", func(t *testing.T) [][]byte {
			var out [][]byte
			for i := 1; i <= 5; i++ {
				out = append(out, sizedPNG(t, i, 1))
			}
			return out
		}, 4, 5, "at most 4"},
		{"dimension cap", func(t *testing.T) [][]byte { return [][]byte{sizedPNG(t, 2049, 1), sizedPNG(t, 1, 1)} }, 1, 2, "2048"},
		{"byte cap", func(t *testing.T) [][]byte {
			a := append(sizedPNG(t, 1, 1), make([]byte, 6<<20)...)
			b := append(sizedPNG(t, 2, 1), make([]byte, 5<<20)...)
			return [][]byte{a, b}
		}, 1, 2, "10 MiB"},
		{"invalid dimensions", func(t *testing.T) [][]byte { return [][]byte{append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)} }, 0, 1, "cannot read image dimensions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k, dir := hostedDeliveryKernel(t)
			pics := tc.pics(t)
			var stream strings.Builder
			var items []string
			for i, pic := range pics {
				item := fmt.Sprintf(`{"type":"image_generation_call","id":"ig_%d","status":"completed","result":"%s"}`, i, base64.StdEncoding.EncodeToString(pic))
				items = append(items, item)
				fmt.Fprintf(&stream, "data: {\"type\":\"response.output_item.done\",\"output_index\":%d,\"item\":%s}\n\n", i, item)
			}
			fmt.Fprintf(&stream, `data: {"type":"response.completed","response":{"status":"completed","output":[%s,{"type":"message","content":[{"type":"output_text","text":"请查看生图回执。"}]}]}}`+"\n\n", strings.Join(items, ","))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, stream.String())
			}))
			defer server.Close()
			p := &provider.Responses{Client: provider.Client{BaseURL: server.URL, APIKey: "fake", Model: "model", HTTPClient: server.Client()}}
			ctx := (&turnRun{k: k, t: k.turn}).modelContext()
			msg, err := p.Chat(ctx, provider.Request{Tools: []provider.ToolDef{{Name: "shell"}}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(k.turn.outImages) != 0 || k.turn.outBytes != 0 {
				t.Fatal("Save automatically queued outgoing images")
			}
			files, err := os.ReadDir(filepath.Join(dir, "generated"))
			if err != nil || len(files) != tc.wantFiles {
				t.Fatalf("saved files = %d: %v", len(files), err)
			}
			var paths []string
			for _, part := range msg.Content {
				if part.Hosted != nil && part.Hosted.Name == "image_generation" {
					rel := strings.TrimSuffix(strings.TrimPrefix(part.Text, "[厂商生图: 已保存到工作区 "), "]")
					if !strings.HasPrefix(rel, "generated/") {
						t.Fatalf("dishonest save receipt: %s", part.Text)
					}
					paths = append(paths, rel)
				}
			}
			if len(paths) != tc.wantFiles {
				t.Fatalf("receipt paths = %v", paths)
			}
			var failures []string
			for _, path := range paths {
				args, _ := json.Marshal(map[string]any{"text": "看图", "images": []string{path, path}})
				if _, err := (notifyTool{k}).Run(ctx, args); err != nil {
					failures = append(failures, err.Error())
				}
			}
			if tc.failure == "" && len(failures) > 0 || tc.failure != "" && !strings.Contains(strings.Join(failures, "\n"), tc.failure) {
				t.Fatalf("notify failures = %v, want %q", failures, tc.failure)
			}
			if _, err = k.recordTurn(k.turn, msg); err != nil {
				t.Fatal(err)
			}
			entries := entriesFor(t, k.Bot)
			if len(entries) != 1 || len(entries[0].Images) != tc.wantImages {
				t.Fatalf("reply entries = %+v", entries)
			}
			for _, f := range files {
				if _, err := os.Stat(filepath.Join(dir, "generated", f.Name())); err != nil {
					t.Fatalf("delivery removed saved file: %v", err)
				}
			}
			if len(k.turn.outImages) != 0 || k.turn.outBytes != 0 {
				t.Fatal("reply did not drain queue")
			}
		})
	}
}

func hostedDeliveryKernel(t *testing.T) (*Kernel, string) {
	t.Helper()
	k, dir, _ := outImageKernel(t)
	if err := config.Add(k.Bot.Root, config.Provider{Name: "openai", Protocol: config.Responses, BaseURL: "http://127.0.0.1:1", OAuth: "chatgpt"}, config.ModelSpec{Name: "model", Context: 100000}); err != nil {
		t.Fatal(err)
	}
	if err := k.Bot.SetBinding("openai", "model"); err != nil {
		t.Fatal(err)
	}
	if err := config.SetWebImageGen(k.Bot.Root, true, "openai"); err != nil {
		t.Fatal(err)
	}
	v, err := config.OAuthVault(k.Bot.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.SetOAuth(v, "chatgpt", vault.OAuthCredential{AccessToken: "fake"}, []string{"127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	k.turn = &turn{}
	k.turn.runCtx = k.withImageGen(context.WithValue(context.Background(), turnKey{}, k.turn))
	return k, dir
}

func TestHostedImageDeliveryStaysWithOriginatingReply(t *testing.T) {
	k, _ := hostedDeliveryKernel(t)
	owner := k.turn
	r := &turnRun{k: k, t: owner}
	oldCtx := r.modelContext()
	if _, err := k.recordTurn(owner, provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("先前的回复")}); err != nil {
		t.Fatal(err)
	}
	// A different active turn must never receive the old request's image.
	k.turn = &turn{}
	pic := base64.StdEncoding.EncodeToString(sizedPNG(t, 1, 1))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, `data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"image_generation_call","id":"ig","status":"completed","result":"%s"},{"type":"message","content":[{"type":"output_text","text":"画好了"}]}]}}`+"\n\n", pic)
	}))
	defer server.Close()
	p := &provider.Responses{Client: provider.Client{BaseURL: server.URL, APIKey: "fake", Model: "model", HTTPClient: server.Client()}}
	chat := func(ctx context.Context) provider.Message {
		t.Helper()
		msg, err := p.Chat(ctx, provider.Request{Tools: []provider.ToolDef{{Name: "shell"}}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return msg
	}
	late := chat(oldCtx)
	if strings.Contains(late.PortableText(), "失败") || len(owner.outImages) != 0 || len(k.turn.outImages) != 0 {
		t.Fatalf("late save contaminated reply: %s", late.PortableText())
	}
	// A late save is only a file. An old notify call cannot attach it, while
	// a fresh call in the same long-lived turn can use that actual receipt.
	rel := strings.TrimSuffix(strings.TrimPrefix(late.Content[0].Text, "[厂商生图: 已保存到工作区 "), "]")
	args, _ := json.Marshal(map[string]any{"text": "看图", "images": []string{rel}})
	if _, err := (notifyTool{k}).Run(oldCtx, args); err == nil || !strings.Contains(err.Error(), "already sent") {
		t.Fatalf("stale notify result: %v", err)
	}
	if _, err := (notifyTool{k}).Run(r.modelContext(), args); err != nil {
		t.Fatal(err)
	}
	continued := provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("已用挂图方法挂上。")}
	if len(owner.outImages) != 1 || len(k.turn.outImages) != 0 {
		t.Fatalf("continuation delivery: %s", continued.PortableText())
	}
	if _, err := k.recordTurn(owner, continued); err != nil {
		t.Fatal(err)
	}
	entries := entriesFor(t, k.Bot)
	if len(entries) != 2 || len(entries[0].Images) != 0 || len(entries[1].Images) != 1 {
		t.Fatalf("reply attachment rows: %+v", entries)
	}
}
