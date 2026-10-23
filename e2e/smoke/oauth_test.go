//go:build smoke

package smoke

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"xbot/internal/config"
	"xbot/internal/vault"
)

// Optional harness mode; all six journey assertions remain unchanged. Tokens
// are generated at runtime and the upstream verifies the actual bearer header.
type smokeOAuth struct {
	mu               sync.Mutex
	access, refresh  string
	calls, refreshes int
}

func smokeToken() string {
	var b [32]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func (s *Server) prepareOAuth(stub *Stub) {
	kind := os.Getenv("XBOT_SMOKE_OAUTH")
	if kind == "" {
		return
	}
	if kind != "openrouter" && kind != "chatgpt" {
		s.t.Fatal("invalid XBOT_SMOKE_OAUTH")
	}
	stub.oauth = &smokeOAuth{access: smokeToken(), refresh: smokeToken()}
	backend, err := config.OAuthVault(s.Root)
	if err != nil {
		s.t.Fatal(err)
	}
	c := vault.OAuthCredential{AccessToken: stub.oauth.access}
	protocol := config.ChatCompletions
	if kind == "chatgpt" {
		protocol = config.Responses
		c.RefreshToken = stub.oauth.refresh
		c.ExpiresAt = time.Now().Add(-time.Minute)
		c.TokenURL = stub.BaseURL() + "/oauth/token"
		c.ClientID = "smoke-client"
		c.Resource = "https://api.openai.com/v1"
		c.Scope = "chatgpt.tokens.use.direct"
	}
	if err := vault.SetOAuth(backend, kind, c, []string{"127.0.0.1"}); err != nil {
		s.t.Fatal(err)
	}
	if err := config.SaveOAuthProvider(s.Root, config.Provider{Name: kind, OAuth: kind, Protocol: protocol, BaseURL: stub.BaseURL(), Models: []config.ModelSpec{{Name: "smoke", Context: 1000000}}}); err != nil {
		s.t.Fatal(err)
	}
	s.t.Cleanup(func() {
		if s.t.Failed() {
			paths, _ := filepath.Glob(filepath.Join(s.Root, "bots", "*", "agents", "*", "result.txt"))
			for _, path := range paths {
				raw, _ := os.ReadFile(path)
				text := backend.Redact(string(raw))
				s.t.Logf("OAuth subagent result: %s", text)
			}
			for _, record := range stub.Requests() {
				var req stubRequest
				_ = json.Unmarshal([]byte(record.Body), &req)
				cue := lastUser(req)
				if len(cue) > 500 {
					cue = cue[:500]
				}
				s.t.Logf("OAuth script caller=%s last user=%q", record.Caller, cue)
			}
		}
		stub.oauth.mu.Lock()
		defer stub.oauth.mu.Unlock()
		if stub.oauth.calls == 0 {
			s.t.Error("no OAuth model request observed")
		}
		if kind == "chatgpt" && stub.oauth.refreshes != 1 {
			s.t.Errorf("refreshes=%d, want 1", stub.oauth.refreshes)
		}
		s.t.Logf("OAuth %s: authenticated requests=%d, refreshes=%d", kind, stub.oauth.calls, stub.oauth.refreshes)
	})
}

func (s *Stub) oauthModel(w http.ResponseWriter, r *http.Request) {
	if s.oauth != nil {
		s.oauth.mu.Lock()
		valid := r.Header.Get("Authorization") == "Bearer "+s.oauth.access
		if valid {
			s.oauth.calls++
		}
		s.oauth.mu.Unlock()
		if !valid {
			http.Error(w, "OAuth bearer missing", 401)
			return
		}
	}
	if r.URL.Path == "/v1/responses" {
		s.oauthResponses(w, r)
		return
	}
	s.handle(w, r)
}

func (s *Stub) oauthRefresh(w http.ResponseWriter, r *http.Request) {
	if s.oauth == nil {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()
	s.oauth.mu.Lock()
	defer s.oauth.mu.Unlock()
	if r.Form.Get("refresh_token") != s.oauth.refresh || r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("client_id") != "smoke-client" {
		http.Error(w, "invalid refresh", 400)
		return
	}
	s.oauth.access = smokeToken()
	s.oauth.refresh = smokeToken()
	s.oauth.refreshes++
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": s.oauth.access, "refresh_token": s.oauth.refresh, "token_type": "Bearer", "expires_in": 3600})
}

// Translate the wire format only at the scripted upstream, so the same
// behavioral script can exercise the real Responses adapter and local tools.
func (s *Stub) oauthResponses(w http.ResponseWriter, r *http.Request) {
	// Response input message content is a string in these text-only journeys.
	var body struct {
		Input  []json.RawMessage `json:"input"`
		Store  bool              `json:"store"`
		Stream bool              `json:"stream"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || body.Store || !body.Stream {
		http.Error(w, "bad Responses request", 400)
		return
	}
	var req stubRequest
	for _, raw := range body.Input {
		var item struct {
			Type      string          `json:"type"`
			Role      string          `json:"role"`
			Content   json.RawMessage `json:"content"`
			CallID    string          `json:"call_id"`
			Name      string          `json:"name"`
			Arguments string          `json:"arguments"`
			Output    string          `json:"output"`
		}
		if json.Unmarshal(raw, &item) != nil {
			http.Error(w, "bad input", 400)
			return
		}
		switch item.Type {
		case "function_call":
			call, _ := json.Marshal(map[string]any{"id": item.CallID, "type": "function", "function": map[string]string{"name": item.Name, "arguments": item.Arguments}})
			req.Messages = append(req.Messages, stubMessage{Role: "assistant", ToolCalls: []json.RawMessage{call}})
		case "function_call_output":
			req.Messages = append(req.Messages, stubMessage{Role: "tool", Content: item.Output})
		default:
			var text string
			if json.Unmarshal(item.Content, &text) != nil {
				var parts []struct {
					Text string `json:"text"`
				}
				_ = json.Unmarshal(item.Content, &parts)
				for _, p := range parts {
					text += p.Text
				}
			}
			req.Messages = append(req.Messages, stubMessage{Role: item.Role, Content: text})
		}
	}
	raw, _ := json.Marshal(req)
	out := r.Clone(r.Context())
	out.Body = io.NopCloser(bytes.NewReader(raw))
	s.handle(&responseWriter{ResponseWriter: w}, out)
}

type responseWriter struct {
	http.ResponseWriter
	text   strings.Builder
	calls  []any
	buffer string
}

func (w *responseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (w *responseWriter) event(v any) {
	raw, _ := json.Marshal(v)
	fmt.Fprintf(w.ResponseWriter, "data: %s\n\n", raw)
	w.Flush()
}
func (w *responseWriter) Write(p []byte) (int, error) {
	w.buffer += string(p)
	for {
		at := strings.Index(w.buffer, "\n\n")
		if at < 0 {
			break
		}
		line := strings.TrimPrefix(w.buffer[:at], "data: ")
		w.buffer = w.buffer[at+2:]
		if line == "[DONE]" {
			output := w.calls
			if w.text.Len() > 0 {
				output = append(output, map[string]any{"type": "message", "id": "smoke_message", "role": "assistant", "content": []any{map[string]string{"type": "output_text", "text": w.text.String()}}})
			}
			w.event(map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed", "output": output}})
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
					Calls   []struct {
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(line), &chunk) != nil {
			return 0, fmt.Errorf("bad scripted event")
		}
		for _, c := range chunk.Choices {
			if c.Delta.Content != "" {
				w.text.WriteString(c.Delta.Content)
				w.event(map[string]string{"type": "response.output_text.delta", "delta": c.Delta.Content})
			}
			for _, call := range c.Delta.Calls {
				w.calls = append(w.calls, map[string]string{"type": "function_call", "id": call.ID, "call_id": call.ID, "name": call.Function.Name, "arguments": call.Function.Arguments})
			}
		}
	}
	return len(p), nil
}
