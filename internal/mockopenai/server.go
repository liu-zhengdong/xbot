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
	var sawShell, sawWrite, sawRemember, sawNotify, sawRecall bool
	var lastTool, lastUser string
	for _, m := range req.Messages {
		if m.Role == "user" {
			lastUser = m.Content
		}
		if m.Role == "tool" {
			lastTool = m.Content
		}
		for _, tc := range m.ToolCalls {
			switch tc.Function.Name {
			case "shell":
				sawShell = true
			case "write_file":
				sawWrite = true
			case "remember":
				sawRemember = true
			case "notify_owner":
				sawNotify = true
			case "recall":
				sawRecall = true
			}
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	switch {
	case strings.Contains(lastUser, "REMEMBER_PERSON") && !sawRemember:
		writeTools(w, []toolSpec{
			{"call_rem_person", "remember", `{"fact":"Ada likes tea","scope":"person","tier":"profile"}`},
			{"call_rem_rel", "remember", `{"fact":"secret handshake","scope":"bot-person","tier":"profile"}`},
		})
		return
	case strings.Contains(lastUser, "REMEMBER_PERSON"):
		writeText(w, "remembered\n")
		return
	case strings.Contains(lastUser, "RECALL_PERSON") && !sawRecall:
		writeTool(w, "call_recall", "recall", `{"query":"tea","scope":"person"}`)
		return
	case strings.Contains(lastUser, "RECALL_PERSON"):
		writeText(w, "recalled\n")
		return
	case strings.Contains(lastUser, "<routine") && strings.Contains(lastUser, "QUIET"):
		writeText(w, "SILENT\n")
		return
	case strings.Contains(lastUser, "<routine") && !sawNotify:
		writeTool(w, "call_notify", "notify_owner", `{"text":"mail is waiting"}`)
		return
	case strings.Contains(lastUser, "<routine"):
		writeText(w, "noted\n")
		return
	}
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
	writeText(w, "Summary written to summary.txt.\n")
}

type toolSpec struct {
	id, name, args string
}

func writeText(w http.ResponseWriter, text string) {
	writeData(w, map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": text}}}})
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flush(w)
}

func writeTools(w http.ResponseWriter, calls []toolSpec) {
	tcs := make([]any, len(calls))
	for i, c := range calls {
		tcs[i] = map[string]any{
			"index": i,
			"id":    c.id,
			"type":  "function",
			"function": map[string]any{
				"name":      c.name,
				"arguments": c.args,
			},
		}
	}
	writeData(w, map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{
		"role":       "assistant",
		"tool_calls": tcs,
	}}}})
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
