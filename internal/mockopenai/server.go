// Package mockopenai is a scripted OpenAI-compatible server for tests.
// The first completion requests a directory listing, the second writes summary.txt,
// and the third returns a final answer. It has no model.
package mockopenai

import (
	"encoding/json"
	"fmt"
	"io"
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
	h := func(w http.ResponseWriter, r *http.Request) { serve(w, r, dir, nil) }
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

// NewScriptMux is NewMux plus a Script that records bodies and holds selected steps.
func NewScriptMux(dir string, script *Script) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	h := func(w http.ResponseWriter, r *http.Request) { serve(w, r, dir, script) }
	mux.HandleFunc("/v1/chat/completions", h)
	mux.HandleFunc("/chat/completions", h)
	return mux
}

func serve(w http.ResponseWriter, r *http.Request, dir string, script *Script) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"message": "missing api key"}})
		return
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"message": err.Error()}})
		return
	}
	if script != nil {
		script.add(string(raw))
	}
	var req incoming
	if err := json.Unmarshal(raw, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"message": err.Error()}})
		return
	}
	var sawShell, sawWrite, sawRemember, sawNotify, sawRecall, sawSpawn, sawPeer bool
	var lastTool, lastUser string
	for _, m := range req.Messages {
		if m.Role == "user" {
			lastUser = m.Content
			lastTool = ""
			sawShell, sawWrite, sawRemember, sawNotify, sawRecall, sawSpawn, sawPeer = false, false, false, false, false, false, false
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
			case "spawn":
				sawSpawn = true
			case "message_bot":
				sawPeer = true
			}
		}
	}
	all := joined(req)
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
	case strings.Contains(lastUser, "DRINK_WATER") && !sawNotify:
		writeTool(w, "call_notify", "notify_owner", `{"text":"drink water","importance":"low"}`)
		return
	case strings.Contains(lastUser, "DRINK_WATER"):
		writeText(w, "noted\n")
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
	case strings.TrimSpace(lastUser) == "ping":
		writeText(w, "pong\n")
		return
	case strings.TrimSpace(lastUser) == "MARKER_7f3a":
		writeText(w, "ok\n")
		return
	case strings.Contains(lastUser, "SPAWN_LONG") && !sawSpawn:
		writeTool(w, "call_spawn", "spawn", `{"task":"LONG_TASK","mode":"brief"}`)
		return
	case strings.Contains(lastUser, "SPAWN_LONG"):
		writeText(w, "started\n")
		return
	case strings.Contains(lastUser, "SPAWN_STEER") && !sawSpawn:
		writeTool(w, "call_spawn", "spawn", `{"task":"STEER_TASK","mode":"brief"}`)
		return
	case strings.Contains(lastUser, "SPAWN_STEER"):
		writeText(w, "started\n")
		return
	case strings.Contains(lastUser, "SPAWN_INTERRUPT") && !sawSpawn:
		writeTool(w, "call_spawn", "spawn", `{"task":"INTERRUPT_TASK","mode":"brief"}`)
		return
	case strings.Contains(lastUser, "SPAWN_INTERRUPT"):
		writeText(w, "started\n")
		return
	case strings.Contains(lastUser, "SPAWN_CANCEL") && !sawSpawn:
		writeTool(w, "call_spawn", "spawn", `{"task":"CANCEL_TASK","mode":"brief"}`)
		return
	case strings.Contains(lastUser, "SPAWN_CANCEL"):
		writeText(w, "started\n")
		return
	case strings.Contains(lastUser, "SPAWN_FORK") && !sawSpawn:
		writeTool(w, "call_spawn", "spawn", `{"task":"FORK_TASK","mode":"fork"}`)
		return
	case strings.Contains(lastUser, "SPAWN_FORK"):
		writeText(w, "started\n")
		return
	case strings.Contains(lastUser, "SPAWN_REMEMBER") && !sawSpawn:
		writeTool(w, "call_spawn", "spawn", `{"task":"REMEMBER_TASK","mode":"brief"}`)
		return
	case strings.Contains(lastUser, "SPAWN_REMEMBER"):
		writeText(w, "started\n")
		return
	case strings.Contains(lastUser, "REMEMBER_TASK"):
		writeText(w, "subagent finished\n")
		return
	case strings.Contains(lastUser, `source="subagent"`) && strings.Contains(lastUser, "subagent finished") && !sawRemember:
		writeTool(w, "call_rem_sub", "remember", `{"fact":"owner likes water","scope":"person","tier":"profile"}`)
		return
	case strings.Contains(lastUser, `source="subagent"`) && strings.Contains(lastUser, "subagent finished"):
		writeText(w, "remembered\n")
		return
	case strings.Contains(lastUser, "SPAWN_BRIEF") && !sawSpawn:
		writeTool(w, "call_spawn", "spawn", `{"task":"BRIEF_TASK","mode":"brief"}`)
		return
	case strings.Contains(lastUser, "SPAWN_BRIEF"):
		writeText(w, "started\n")
		return
	case strings.Contains(all, "STEER_MARK"):
		writeText(w, "kept prior work\n")
		return
	case strings.Contains(all, "INTERRUPT_NOW"):
		writeText(w, "restarted after interrupt\n")
		return
	case strings.Contains(lastUser, "long task done"):
		writeText(w, "saw subagent done\n")
		return
	case strings.Contains(lastUser, "LONG_TASK") && script != nil:
		script.Enter("long")
		_ = script.Wait("long", r.Context())
		writeText(w, "long task done\n")
		return
	case strings.Contains(lastUser, "STEER_TASK") && !sawShell:
		writeTool(w, "call_prior", "shell", `{"command":"printf PRIOR_WORK"}`)
		return
	case strings.Contains(lastUser, "STEER_TASK") && sawShell && script != nil:
		script.Enter("steer")
		_ = script.Wait("steer", r.Context())
		writeTool(w, "call_still", "shell", `{"command":"printf STILL_GOING"}`)
		return
	case strings.Contains(lastUser, "INTERRUPT_TASK") && !sawShell:
		writeTool(w, "call_step", "shell", `{"command":"printf PRIOR_STEP"}`)
		return
	case strings.Contains(lastUser, "INTERRUPT_TASK") && script != nil:
		script.Enter("intr")
		_ = script.Wait("intr", r.Context())
		return
	case strings.Contains(lastUser, "CANCEL_TASK") && script != nil:
		script.Enter("cancel")
		_ = script.Wait("cancel", r.Context())
		return
	case strings.Contains(lastUser, "FORK_TASK"):
		writeText(w, "fork done\n")
		return
	case strings.Contains(lastUser, "BRIEF_TASK"):
		writeText(w, "brief done\n")
		return
	case strings.Contains(lastUser, `source="webhook`) && !sawNotify:
		writeTool(w, "call_notify", "notify_owner", `{"text":"hook fired"}`)
		return
	case strings.Contains(lastUser, `source="webhook`):
		writeText(w, "noted\n")
		return
	case strings.Contains(lastUser, `source="watch`) && !sawNotify:
		writeTool(w, "call_notify", "notify_owner", `{"text":"file changed"}`)
		return
	case strings.Contains(lastUser, `source="watch`):
		writeText(w, "noted\n")
		return
	case strings.Contains(lastUser, "NEED_APPROVAL") && !sawShell:
		writeTool(w, "call_mkdir", "shell", `{"command":"mkdir -p approved-dir"}`)
		return
	case strings.Contains(lastUser, "NEED_APPROVAL"):
		writeText(w, "made the directory\n")
		return
	case strings.Contains(lastUser, "HELLO_WEB"):
		writeText(w, "web hello\n")
		return
	case strings.Contains(lastUser, "FROM_TERMINAL"):
		writeText(w, "terminal hello\n")
		return
	case strings.Contains(lastUser, "MSG_BOB") && !sawPeer:
		writeTool(w, "call_peer", "message_bot", `{"bot":"bob","text":"hello from ada"}`)
		return
	case strings.Contains(lastUser, "MSG_BOB"):
		writeText(w, "sent\n")
		return
	case strings.Contains(lastUser, "hello from ada") && !sawPeer:
		writeTool(w, "call_peer", "message_bot", `{"bot":"ada","text":"reply from bob"}`)
		return
	case strings.Contains(lastUser, "hello from ada"):
		writeText(w, "sent\n")
		return
	case strings.Contains(lastUser, "reply from bob"):
		writeText(w, "got reply\n")
		return
	case strings.Contains(lastUser, "LOOP") && !sawPeer:
		other := "bob"
		if scanBotName(req) == "bob" {
			other = "ada"
		}
		writeTool(w, "call_peer", "message_bot", fmt.Sprintf(`{"bot":%q,"text":"LOOP"}`, other))
		return
	case strings.Contains(lastUser, "LOOP"):
		writeText(w, "stopped\n")
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

func scanBotName(req incoming) string {
	const mark = "BOTNAME="
	for _, m := range req.Messages {
		i := strings.Index(m.Content, mark)
		if i < 0 {
			continue
		}
		rest := m.Content[i+len(mark):]
		var b strings.Builder
		for _, c := range rest {
			switch {
			case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
				b.WriteRune(c)
			default:
				return b.String()
			}
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	return ""
}

func joined(req incoming) string {
	var b strings.Builder
	for _, m := range req.Messages {
		b.WriteString(m.Content)
		b.WriteByte('\n')
	}
	return b.String()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
