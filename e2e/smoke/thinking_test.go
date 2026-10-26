//go:build smoke

package smoke

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/config"
	"xbot/internal/work"
)

// The real CLI configures thinking, the real serve executes two file tools,
// and a strict scripted model requires every previous reasoning block back.
// A second owner turn after a process restart exercises persisted replay.
func TestThinkingToolReplay(t *testing.T) {
	for _, protocol := range []string{"chat_completions", "anthropic_messages", "responses"} {
		t.Run(protocol, func(t *testing.T) {
			srv := Start(t, StartStub(t))
			srv.CreateBot("ada", "Be brief.")
			workspace, _, err := work.Open(srv.Root, "ada")
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"one", "two"} {
				if err := os.WriteFile(filepath.Join(workspace, name), []byte("file-result-"+name), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var mu sync.Mutex
			calls := 0
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					http.Error(w, err.Error(), 400)
					return
				}
				mu.Lock()
				defer mu.Unlock()
				n := calls + 1
				raw, _ := json.Marshal(body)
				history := string(raw)
				fail := func(why string) { t.Errorf("request %d: %s", n, why); http.Error(w, why, 400) }
				if protocol == "responses" {
					if th, _ := body["reasoning"].(map[string]any); th["effort"] != "low" {
						fail("native effort is not low")
						return
					}
					if body["thinking"] != nil || body["reasoning_effort"] != nil {
						fail("foreign thinking controls")
						return
					}
				} else {
					th, _ := body["thinking"].(map[string]any)
					if th["type"] != "enabled" {
						fail("thinking not enabled")
						return
					}
					if protocol == "chat_completions" && body["reasoning_effort"] != "low" {
						fail("effort is not low")
						return
					}
				}
				for prev := 1; prev < n; prev++ {
					if !strings.Contains(history, fmt.Sprintf("PRIVATE-THOUGHT-%d", prev)) {
						fail(fmt.Sprintf("reasoning %d missing", prev))
						return
					}
					if protocol == "anthropic_messages" && !strings.Contains(history, fmt.Sprintf("signature-%d", prev)) {
						fail("thinking signature missing")
						return
					}
				}
				// Tool results must come from the real file tools, including after restart.
				if n%3 != 1 && !strings.Contains(history, "file-result-one") {
					fail("first file tool did not execute")
					return
				}
				if n%3 == 0 && !strings.Contains(history, "file-result-two") {
					fail("second file tool did not execute")
					return
				}
				calls++
				id := fmt.Sprintf("call_%d", n)
				thought := fmt.Sprintf("PRIVATE-THOUGHT-%d", n)
				filename := "one"
				if n%3 == 2 {
					filename = "two"
				}
				args := map[string]any{"path": filename}
				argJSON, _ := json.Marshal(args)
				final := n%3 == 0
				answer := "文件已读完。"
				w.Header().Set("Content-Type", "text/event-stream")
				event := func(v any) { data, _ := json.Marshal(v); fmt.Fprintf(w, "data: %s\n\n", data) }
				switch protocol {
				case "chat_completions":
					event(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"reasoning_content": thought}}}})
					delta := map[string]any{"content": answer}
					if !final {
						delta = map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": id, "type": "function", "function": map[string]any{"name": "read_file", "arguments": string(argJSON)}}}}
					}
					event(map[string]any{"choices": []any{map[string]any{"delta": delta}}})
					fmt.Fprint(w, "data: [DONE]\n\n")
				case "anthropic_messages":
					event(map[string]any{"type": "message_start"})
					blocks := []any{map[string]any{"type": "thinking", "thinking": thought, "signature": fmt.Sprintf("signature-%d", n)}}
					if final {
						blocks = append(blocks, map[string]any{"type": "text", "text": ""})
					} else {
						blocks = append(blocks, map[string]any{"type": "tool_use", "id": id, "name": "read_file", "input": args})
					}
					for i, b := range blocks {
						event(map[string]any{"type": "content_block_start", "index": i, "content_block": b})
						if final && i == 1 {
							event(map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "text_delta", "text": answer}})
						}
						event(map[string]any{"type": "content_block_stop", "index": i})
					}
					stop := "tool_use"
					if final {
						stop = "end_turn"
					}
					event(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stop}})
					event(map[string]any{"type": "message_stop"})
				case "responses":
					if final {
						event(map[string]any{"type": "response.output_text.delta", "delta": answer})
					}
					output := []any{map[string]any{"type": "reasoning", "id": fmt.Sprintf("rs_%d", n), "summary": []any{}, "encrypted_content": thought}}
					if final {
						output = append(output, map[string]any{"type": "message", "id": id, "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": answer}}})
					} else {
						output = append(output, map[string]any{"type": "function_call", "id": id, "call_id": id, "name": "read_file", "arguments": string(argJSON)})
					}
					event(map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed", "output": output}})
				}
			}))
			defer model.Close()
			modelName := "deepseek-flash"
			if protocol == "anthropic_messages" {
				modelName = "claude-sonnet-4-5"
			}
			if protocol == "responses" {
				modelName = "gpt-5.5"
			}
			// Exercise the actual flag parser, persistence and binding resolver.
			cmd := exec.Command(xbotBinary, "--root", srv.Root, "provider", "add", "thinking", "--base-url", model.URL, "--api-key-env", "XBOT_API_KEY", "--model", modelName, "--context", "1000000", "--protocol", protocol, "--thinking", "on", "--hosted-web-search", "off")
			cmd.Env = srv.env
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("provider add: %v %s", err, out)
			}
			if status, body := srv.do("PUT", "/api/bots/ada/binding", map[string]string{"provider": "thinking", "model": modelName}); status != 200 {
				t.Fatalf("bind: %d %s", status, body)
			}
			for turn := 0; turn < 2; turn++ {
				if turn == 1 {
					srv.Restart()
				}
				reply := srv.Send("ada", fmt.Sprintf("读 one 和 two，轮次 %d", turn))
				if reply.Done != "文件已读完。" || len(reply.Tools) != 2 {
					t.Fatalf("result: %+v", reply)
				}
				_, history := srv.do("GET", "/api/bots/ada/transcript", nil)
				stream, _ := json.Marshal(reply)
				for _, raw := range [][]byte{history, stream} {
					if strings.Contains(string(raw), "PRIVATE-THOUGHT") || strings.Contains(string(raw), "signature-") || strings.Contains(string(raw), "vendor_raw") {
						t.Fatalf("private reasoning exposed: %s", raw)
					}
				}
				t.Logf("turn %d total latency %s (scripted model, includes two real file tools)", turn, time.Since(reply.Sent))
			}
			mu.Lock()
			defer mu.Unlock()
			if calls != 6 {
				t.Fatalf("model calls=%d, want 6", calls)
			}
		})
	}
}

// Check the shipped flag parser and persisted values without a model call.
func TestThinkingEffortCLI(t *testing.T) {
	root := t.TempDir()
	run := func(effort string) error {
		args := []string{"--root", root, "provider", "add", "ds", "--base-url", "http://127.0.0.1:1", "--api-key-env", "FAKE_KEY", "--model", "deepseek-flash", "--context", "1000000", "--thinking", "on"}
		if effort != "" {
			args = append(args, "--reasoning-effort", effort)
		}
		out, err := exec.Command(xbotBinary, args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%w: %s", err, out)
		}
		return nil
	}
	for _, effort := range []string{"low", "high", "max"} {
		if err := run(effort); err != nil {
			t.Fatalf("%s: %v", effort, err)
		}
		if err := run(""); err != nil {
			t.Fatal(err)
		}
		p, _, err := config.Lookup(root, "ds", "deepseek-flash")
		if err != nil || len(p.Models) != 1 || p.Models[0].ReasoningEffort != effort {
			t.Fatalf("did not preserve %s: %v", effort, err)
		}
	}
	if err := run("medium"); err == nil {
		t.Fatal("accepted nonexistent Flash effort medium")
	}
}
