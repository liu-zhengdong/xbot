// Package mockopenai is a scripted OpenAI-compatible server for tests.
// The first completion requests a directory listing, the second writes summary.txt,
// and the third returns a final answer. It has no model.
package mockopenai

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
)

// NewMux serves /health and the chat completions endpoint.
// dir is the workspace used in the scripted shell and write_file calls.
// An empty dir uses relative paths, for a caller that has already changed directory.
func NewMux(dir string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	h := func(w http.ResponseWriter, r *http.Request) { serve(w, r, dir) }
	mux.HandleFunc("/v1/chat/completions", h)
	mux.HandleFunc("/chat/completions", h)
	return mux
}

type incoming struct {
	Messages []struct {
		Role      string `json:"role"`
		Content   string `json:"content"`
		ToolCalls []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tool_calls"`
	} `json:"messages"`
}

func serve(w http.ResponseWriter, r *http.Request, dir string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"message": "missing api key"}})
		return
	}
	var req incoming
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"message": err.Error()}})
		return
	}
	var sawShell, sawWrite bool
	var lastTool string
	for _, m := range req.Messages {
		if m.Role == "tool" {
			lastTool = m.Content
		}
		for _, tc := range m.ToolCalls {
			switch tc.Function.Name {
			case "shell":
				sawShell = true
			case "write_file":
				sawWrite = true
			}
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	if !sawShell {
		cmd := "ls -1"
		if dir != "" {
			cmd = "ls -1 " + shellQuote(dir)
		}
		args, _ := json.Marshal(map[string]string{"command": cmd})
		writeTool(w, "call_ls", "shell", string(args))
		return
	}
	if !sawWrite {
		path := "summary.txt"
		if dir != "" {
			path = filepath.Join(dir, "summary.txt")
		}
		args, _ := json.Marshal(map[string]string{
			"path":    path,
			"content": "files:\n" + lastTool,
		})
		writeTool(w, "call_write", "write_file", string(args))
		return
	}
	writeData(w, map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": "Summary written "}}}})
	writeData(w, map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "to summary.txt.\n"}}}})
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flush(w)
}

func writeTool(w http.ResponseWriter, id, name, args string) {
	writeData(w, map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{
		"role": "assistant",
		"tool_calls": []any{map[string]any{
			"index": 0,
			"id":    id,
			"type":  "function",
			"function": map[string]any{
				"name":      name,
				"arguments": args,
			},
		}},
	}}}})
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flush(w)
}

func writeData(w http.ResponseWriter, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", b)
	flush(w)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func flush(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
