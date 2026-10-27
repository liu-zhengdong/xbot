//go:build smoke

package smoke

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/config"
	"xbot/internal/provider"
	"xbot/internal/vault"
	"xbot/internal/work"
)

// The actual serve binary, settings and binding HTTP APIs, OAuth vault
// transport, Responses stream and workspace are real. Only the vendor is
// scripted. These assertions follow the hosted-image journey independently
// of the adapter and kernel implementation.
func TestHostedImageGenerationOAuth(t *testing.T) {
	t.Setenv("XBOT_SMOKE_OAUTH", "")
	const imageModel = "gpt-image-2.5-flare"
	const imageB64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6qS8AAAAASUVORK5CYII="
	png, err := base64.StdEncoding.DecodeString(imageB64)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		kind   string
		attach bool
	}{{"chatgpt", false}, {"chatgpt", true}, {"xai", false}, {"xai", true}} {
		kind := tc.kind
		t.Run(fmt.Sprintf("%s/attach=%v", kind, tc.attach), func(t *testing.T) {
			wantRequests, disabledRequest := 3, 2
			if tc.attach {
				wantRequests, disabledRequest = 5, 4
			}
			stub := StartStub(t)
			srv := Start(t, stub)
			srv.CreateBot("ada", "Be brief.")
			access := smokeToken()
			var mu sync.Mutex
			var requests []struct {
				Tools []map[string]any `json:"tools"`
				Input json.RawMessage  `json:"input"`
			}
			vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer "+access {
					t.Error("wrong hosted-image endpoint or OAuth authentication")
					http.Error(w, "invalid scripted request", 401)
					return
				}
				var req struct {
					Tools []map[string]any `json:"tools"`
					Input json.RawMessage  `json:"input"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					http.Error(w, "bad scripted request", 400)
					return
				}
				mu.Lock()
				requests = append(requests, req)
				n := len(requests)
				mu.Unlock()
				w.Header().Set("Content-Type", "text/event-stream")
				event := func(v any) {
					raw, _ := json.Marshal(v)
					fmt.Fprintf(w, "data: %s\n\n", raw)
				}
				var output []any
				if n == 1 {
					item := map[string]any{"type": "image_generation_call", "id": "ig_smoke", "status": "completed", "result": imageB64}
					if kind == "xai" {
						item["prompt"] = "一只猫"
					} else {
						item["revised_prompt"] = "一只猫"
					}
					event(map[string]any{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"type": "image_generation_call", "id": "ig_smoke", "status": "in_progress"}})
					event(map[string]any{"type": "response.image_generation_call.generating", "item_id": "ig_smoke", "output_index": 0})
					event(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
					output = append(output, item)
					if tc.attach {
						// A normal tool step lets the Bot see the local save receipt
						// on its next model request, within this same user turn.
						checkTool := ""
						for _, tool := range req.Tools {
							if name, _ := tool["name"].(string); name == "bash" || name == "zsh" || name == "sh" {
								checkTool = name
								break
							}
						}
						if checkTool == "" {
							t.Error("no mounted shell to inspect generated directory")
						}
						output = append(output, map[string]any{"type": "function_call", "id": "fc_check", "call_id": "check", "name": checkTool, "arguments": `{"command":"ls generated","description":"查看生成图片的真实文件名"}`})
					}
				} else if tc.attach && n == 2 {
					var replay []map[string]any
					_ = json.Unmarshal(req.Input, &replay)
					for _, item := range replay {
						if item["type"] == "function_call_output" {
							if output, _ := item["output"].(string); strings.HasPrefix(output, "error:") || !strings.Contains(output, ".png") {
								t.Errorf("directory check failed: %s", output)
							}
						}
					}
					// Take the path from the actual replayed receipt, never a
					// guessed filename or a fixture-created paste id.
					rel := regexp.MustCompile(`generated/[a-zA-Z0-9._-]+\.png`).FindString(string(req.Input))
					if rel == "" || !strings.Contains(string(req.Input), "已保存到工作区") {
						t.Error("missing real save receipt before send_message_to_user")
					}
					if strings.Contains(string(req.Input), "已挂本条回复") {
						t.Error("save receipt falsely claims attachment")
					}
					args, _ := json.Marshal(map[string]any{"text": "请看这张图", "images": []string{rel, rel}})
					output = append(output, map[string]any{"type": "function_call", "id": "fc_notify", "call_id": "notify", "name": "send_message_to_user", "arguments": string(args)})
				} else if n == wantRequests {
					// A nominally completed tool with no image must remain an
					// honest failure in the persisted conversation.
					output = append(output, map[string]any{"type": "image_generation_call", "id": "ig_empty", "status": "completed"})
				}
				text := "生成结果已处理。"
				if tc.attach && n == 3 {
					if !strings.Contains(string(req.Input), "picture(s) will show in your reply bubble") {
						t.Error("send_message_to_user did not confirm attachment")
					}
					text = "已用挂图方法挂上。"
				}
				event(map[string]any{"type": "response.output_text.delta", "delta": text})
				output = append(output, map[string]any{"type": "message", "id": fmt.Sprintf("m%d", n), "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": text}}})
				event(map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed", "output": output}})
			}))
			defer vendor.Close()
			backend, err := config.OAuthVault(srv.Root)
			if err != nil {
				t.Fatal(err)
			}
			if err := vault.SetOAuth(backend, kind, vault.OAuthCredential{AccessToken: access, ExpiresAt: time.Now().Add(time.Hour)}, []string{"127.0.0.1"}); err != nil {
				t.Fatal(err)
			}
			model := "gpt-5.5"
			if kind == "xai" {
				model = "grok-4"
			}
			off := false
			if err := config.SaveOAuthProvider(srv.Root, config.Provider{Name: kind, OAuth: kind, Protocol: config.Responses, BaseURL: vendor.URL, Models: []config.ModelSpec{{Name: model, Context: 1000000, HostedSearch: &off}}}); err != nil {
				t.Fatal(err)
			}
			if code, raw := srv.do("PUT", "/api/bots/ada/binding", map[string]string{"provider": kind, "model": model}); code != 200 {
				t.Fatalf("binding: %d %s", code, raw)
			}
			if code, raw := srv.do("PUT", "/api/web-search", map[string]any{"enabled": false, "mode": "auto", "provider": "ddg"}); code != 200 {
				t.Fatalf("disable search: %d %s", code, raw)
			}
			setImage := func(enabled bool) {
				t.Helper()
				selected := "openai"
				if kind == "xai" {
					selected = "xai"
				}
				if code, raw := srv.do("PUT", "/api/image-gen", map[string]any{"enabled": enabled, "provider": selected}); code != 200 {
					t.Fatalf("global image setting: %d %s", code, raw)
				}
				code, raw := srv.do("PUT", "/api/providers/models/image-generation", map[string]any{"provider": kind, "model": model, "enabled": enabled, "image_model": imageModel})
				if code != 200 {
					t.Fatalf("image setting: %d %s", code, raw)
				}
				_, spec, err := config.Lookup(srv.Root, kind, model)
				if err != nil || spec.HasHostedImageGen(config.Responses) != enabled || spec.ImageGenModel != imageModel {
					t.Fatalf("image setting did not persist: %+v %v", spec, err)
				}
			}
			setImage(true)
			first := srv.Send("ada", "画猫并告诉我图在哪。")
			if !tc.attach && len(first.Tools) != 0 {
				t.Fatalf("hosted image ran as a local tool: %+v", first.Tools)
			}
			if tc.attach && (len(first.Tools) != 2 || (first.Tools[0].Name != "sh" && first.Tools[0].Name != "bash" && first.Tools[0].Name != "zsh") || first.Tools[1].Name != "send_message_to_user") {
				t.Fatalf("delivery did not use existing tools: %+v", first.Tools)
			}
			genDir := filepath.Join(work.BotDir(srv.Root, "ada"), "generated")
			files, err := os.ReadDir(genDir)
			if err != nil || len(files) != 1 {
				t.Fatalf("expected exactly one generated image after repeated completion: files=%v err=%v", files, err)
			}
			rel := "generated/" + files[0].Name()
			actual, err := os.ReadFile(filepath.Join(genDir, files[0].Name()))
			if err != nil || !bytes.Equal(actual, png) || !strings.HasSuffix(rel, ".png") {
				t.Fatalf("generated image bytes/type mismatch: len=%d err=%v", len(actual), err)
			}
			id, _ := work.PastedNameFor("image/png", png)
			if code, raw := srv.do("GET", "/api/bots/ada/paste/"+id, nil); tc.attach {
				if code != 200 || !bytes.Equal(raw, png) {
					t.Fatalf("attached image endpoint: %d, bytes=%d", code, len(raw))
				}
			} else if code != 404 {
				t.Fatalf("Save exposed an unrequested image: %d", code)
			}
			setImage(false)
			srv.Send("ada", "确认刚才生成的文件路径。")
			setImage(true)
			srv.Send("ada", "再生成一次。")
			code, raw := srv.do("GET", "/api/bots/ada/transcript", nil)
			var transcript struct {
				Messages []struct {
					Parts  []provider.ContentPart `json:"parts"`
					Images []string               `json:"images"`
				} `json:"messages"`
			}
			if code != 200 || json.Unmarshal(raw, &transcript) != nil {
				t.Fatalf("transcript: %d %s", code, raw)
			}
			foundPath, foundFailure, attached := false, false, 0
			for _, row := range transcript.Messages {
				if len(row.Images) > 0 {
					attached++
					if attached != 1 || len(row.Images) != 1 || row.Images[0] != id {
						t.Fatalf("wrong reply attachment: %v", row.Images)
					}
				}
				for _, part := range row.Parts {
					if part.Hosted == nil || part.Hosted.Name != "image_generation" {
						continue
					}
					if part.Hosted.Status == "completed" && part.Text == "[厂商生图: 已保存到工作区 "+rel+"]" {
						foundPath = true
					}
					if part.Hosted.Status == "failed" && strings.Contains(part.Text, "厂商生图失败") {
						foundFailure = true
					}
				}
			}
			wantAttached := 0
			if tc.attach {
				wantAttached = 1
			}
			if attached != wantAttached || !foundPath || !foundFailure || strings.Contains(string(raw), imageB64) || strings.Contains(string(raw), access) {
				t.Fatalf("transcript lost result/failure or leaked data: path=%v failure=%v", foundPath, foundFailure)
			}
			files, err = os.ReadDir(genDir)
			if err != nil || len(files) != 1 {
				t.Fatalf("disabled/empty generation created a file: files=%v err=%v", files, err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(requests) != wantRequests {
				t.Fatalf("model requests=%d, want %d", len(requests), wantRequests)
			}
			for i, req := range requests {
				imageTools := 0
				for _, tool := range req.Tools {
					if tool["type"] == "web_search" || tool["type"] == "x_search" || tool["name"] == "web_search" {
						t.Fatal("search tool mounted while search disabled")
					}
					if tool["type"] != "image_generation" {
						continue
					}
					imageTools++
					if kind == "chatgpt" && tool["model"] != imageModel {
						t.Fatal("OpenAI tool-level image model missing")
					}
					if kind == "xai" && (tool["model"] != nil || tool["size"] != nil || tool["quality"] != nil) {
						t.Fatal("xAI received OpenAI-specific image options")
					}
				}
				want := 1
				if i+1 == disabledRequest {
					want = 0
				}
				if imageTools != want {
					t.Fatalf("request %d image tools=%d, want %d", i+1, imageTools, want)
				}
				if strings.Contains(string(req.Input), imageB64) || i > 0 && !strings.Contains(string(req.Input), rel) {
					t.Fatalf("request %d did not replay the saved path without base64", i+1)
				}
			}
			t.Logf("scripted %s first visible reply: %s; generated bytes=%d", kind, first.FirstVisible.Sub(first.Sent), len(actual))
		})
	}
}
