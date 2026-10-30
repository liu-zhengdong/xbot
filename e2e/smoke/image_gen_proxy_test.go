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
	"sync/atomic"
	"testing"
	"time"

	"xbot/internal/config"
	"xbot/internal/vault"
	"xbot/internal/work"
)

// Real serve + Completions -> tools door -> OAuth Responses -> generated file
// -> existing send_message_to_user attachment. Only the two models are scripted.
func TestImageGenerationLocalProxy(t *testing.T) {
	for _, kind := range []string{"chatgpt", "xai"} {
		t.Run(kind, func(t *testing.T) {
			const imageB64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6qS8AAAAASUVORK5CYII="
			png, _ := base64.StdEncoding.DecodeString(imageB64)
			var calls atomic.Int32
			chat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Messages json.RawMessage `json:"messages"`
					Tools    []struct {
						Function struct {
							Name string `json:"name"`
						} `json:"function"`
					} `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
				}
				for _, tool := range req.Tools {
					if tool.Function.Name == "generate_image" {
						t.Error("listed tool leaked into tools[]")
					}
				}
				n := calls.Add(1)
				var tool, args string
				switch n {
				case 1:
					tool, args = "tools", `{"action":"search","query":"generate_image"}`
				case 2:
					if !strings.Contains(string(req.Messages), "generate_image") {
						t.Error("proxy undiscoverable")
					}
					tool, args = "tools", `{"action":"describe","id":"image_gen"}`
				case 3:
					tool, args = "tools", `{"action":"call","id":"generate_image","args":{"prompt":"画一只猫"}}`
				case 4:
					rel := regexp.MustCompile(`generated/[a-zA-Z0-9._-]+\.png`).FindString(string(req.Messages))
					if rel == "" || !strings.Contains(string(req.Messages), "已保存到工作区") {
						t.Error("missing real save receipt")
					}
					raw, _ := json.Marshal(map[string]any{"text": "请看这张图", "images": []string{rel}})
					tool, args = "send_message_to_user", string(raw)
				}
				message := map[string]any{"role": "assistant", "content": fmt.Sprintf("处理步骤 %d。", n)}
				reason := "stop"
				if tool != "" {
					message["tool_calls"] = []any{map[string]any{"id": fmt.Sprintf("c%d", n), "type": "function", "function": map[string]any{"name": tool, "arguments": args}}}
					reason = "tool_calls"
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": reason}}})
			}))
			defer chat.Close()
			srv := Start(t, &Stub{srv: chat})
			srv.CreateBot("ada", "Be brief.")
			token := smokeToken()
			var imageCalls atomic.Int32
			vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				imageCalls.Add(1)
				if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer "+token {
					t.Error("proxy did not use selected OAuth Responses")
				}
				var req struct {
					Tools []map[string]any `json:"tools"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
				}
				if len(req.Tools) != 1 || req.Tools[0]["type"] != "image_generation" {
					t.Errorf("proxy tools: %+v", req.Tools)
				}
				if kind == "xai" && len(req.Tools) > 0 && req.Tools[0]["model"] != nil {
					t.Error("OpenAI option sent to xAI")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				item := `{"type":"image_generation_call","id":"ig_proxy","status":"completed","result":"` + imageB64 + `"}`
				fmt.Fprintf(w, "data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":%s}\n\n", item)
				fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[%s]}}\n\n", item)
			}))
			defer vendor.Close()
			backend, err := config.OAuthVault(srv.Root)
			if err != nil {
				t.Fatal(err)
			}
			if err := vault.SetOAuth(backend, kind, vault.OAuthCredential{AccessToken: token, ExpiresAt: time.Now().Add(time.Hour)}, []string{"127.0.0.1"}); err != nil {
				t.Fatal(err)
			}
			off := false
			if err := config.SaveOAuthProvider(srv.Root, config.Provider{Name: kind, OAuth: kind, Protocol: config.Responses, BaseURL: vendor.URL, Models: []config.ModelSpec{{Name: "image-model", Context: 1000000, HostedSearch: &off}}}); err != nil {
				t.Fatal(err)
			}
			selected := "openai"
			if kind == "xai" {
				selected = "xai"
			}
			if code, raw := srv.do("PUT", "/api/image-gen", map[string]any{"enabled": true, "mode": "auto", "provider": selected}); code != 200 {
				t.Fatalf("settings: %d %s", code, raw)
			}
			started := time.Now()
			reply := srv.Send("ada", "画猫并挂到回复里。")
			t.Logf("scripted proxy journey: %s", time.Since(started))
			if reply.Done != "请看这张图" || imageCalls.Load() != 1 {
				_, transcript := srv.do("GET", "/api/bots/ada/transcript", nil)
				t.Logf("transcript: %s", transcript)
				t.Fatalf("reply=%+v image calls=%d chat calls=%d", reply, imageCalls.Load(), calls.Load())
			}
			dir := work.BotDir(srv.Root, "ada")
			files, err := os.ReadDir(filepath.Join(dir, "generated"))
			if err != nil || len(files) != 1 {
				t.Fatalf("generated files %v %v", files, err)
			}
			got, err := os.ReadFile(filepath.Join(dir, "generated", files[0].Name()))
			if err != nil || !bytes.Equal(got, png) {
				t.Fatal("wrong saved image")
			}
			id, _ := work.PastedNameFor("image/png", png)
			if code, raw := srv.do("GET", "/api/bots/ada/paste/"+id, nil); code != 200 || !bytes.Equal(raw, png) {
				t.Fatal("existing attachment delivery failed")
			}
			_, raw := srv.do("GET", "/api/bots/ada/transcript", nil)
			if strings.Contains(string(raw), imageB64) {
				t.Fatal("base64 leaked into transcript")
			}
		})
	}
}
