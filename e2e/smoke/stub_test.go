//go:build smoke

package smoke

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// The stub waits before its first byte, as a real model does, and spaces text
// segments out so the client can see the reply arrive in pieces.
const (
	stubFirstDelay = 300 * time.Millisecond
	stubSegmentGap = 200 * time.Millisecond
)

// Callers the stub recognises from the system message.
const (
	callerForeground = "foreground"
	callerSubagent   = "subagent"
	callerCompaction = "compaction"
	callerExtract    = "extract"
)

const (
	compactionPrefix = "Summarize the conversation so it can continue"
	subagentPrefix   = "You are a subagent working for the main bot."
	extractPrefix    = "Extract durable facts and decisions from the turns."
)

var helloCall = regexp.MustCompile(`SMOKE_HELLO_(\d+)`)

// StubRequest is one model call the stub answered.
type StubRequest struct {
	At     time.Time
	Caller string
	Bot    string
	Body   string
}

// Stub is a scripted OpenAI-compatible server.
type Stub struct {
	srv *httptest.Server

	mu   sync.Mutex
	reqs []StubRequest
}

// StartStub serves /v1/chat/completions until the test ends.
func StartStub(t *testing.T) *Stub {
	t.Helper()
	s := &Stub{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", s.handle)
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

// BaseURL is what XBOT_BASE_URL points at.
func (s *Stub) BaseURL() string { return s.srv.URL + "/v1" }

// Requests returns a copy of the calls seen so far.
func (s *Stub) Requests() []StubRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]StubRequest(nil), s.reqs...)
}

type stubMessage struct {
	Role      string            `json:"role"`
	Content   string            `json:"content"`
	ToolCalls []json.RawMessage `json:"tool_calls"`
}

type stubRequest struct {
	Messages []stubMessage `json:"messages"`
}

func (s *Stub) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req stubRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	caller, bot := classify(req)
	s.mu.Lock()
	s.reqs = append(s.reqs, StubRequest{At: time.Now(), Caller: caller, Bot: bot, Body: string(raw)})
	s.mu.Unlock()

	time.Sleep(stubFirstDelay)
	switch caller {
	case callerCompaction:
		streamText(w, []string{"Summary of the earlier turns."})
	case callerExtract:
		streamText(w, []string{"ok"})
	case callerSubagent:
		streamText(w, []string{"ok"})
	default:
		streamText(w, s.script(req))
	}
}

// classify says who called: compaction and the extractor have fixed system
// prompts, a subagent starts with exec.SubPersona, and everyone else counts as
// a foreground turn for the bot named in its persona.
func classify(req stubRequest) (caller, bot string) {
	for _, m := range req.Messages {
		if m.Role != "system" {
			continue
		}
		sys := strings.TrimSpace(m.Content)
		switch {
		case strings.HasPrefix(sys, compactionPrefix):
			return callerCompaction, ""
		case strings.HasPrefix(sys, subagentPrefix):
			return callerSubagent, botName(sys)
		case strings.HasPrefix(sys, extractPrefix):
			return callerExtract, botName(sys)
		}
	}
	return callerForeground, botName(joined(req))
}

func botName(text string) string {
	const mark = "BOTNAME="
	i := strings.Index(text, mark)
	if i < 0 {
		return ""
	}
	rest := text[i+len(mark):]
	var b strings.Builder
	for _, c := range rest {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
			b.WriteRune(c)
		default:
			return b.String()
		}
	}
	return b.String()
}

func joined(req stubRequest) string {
	var b strings.Builder
	for _, m := range req.Messages {
		b.WriteString(m.Content)
		b.WriteByte('\n')
	}
	return b.String()
}

// lastUser is the newest owner line, which carries the SMOKE_* cue.
func lastUser(req stubRequest) string {
	out := ""
	for _, m := range req.Messages {
		if m.Role == "user" {
			out = m.Content
		}
	}
	return out
}

// script returns the reply segments for one foreground request.
func (s *Stub) script(req stubRequest) []string {
	text := lastUser(req)
	switch {
	case helloCall.MatchString(text):
		n := helloCall.FindStringSubmatch(text)[1]
		return []string{"你好，", "这是第 " + n + " 次", "流式回复"}
	case strings.Contains(text, "SMOKE_FROM_TERMINAL"):
		return []string{"终端你好"}
	case strings.Contains(text, "SMOKE_FROM_WEB_LATE"):
		return []string{"网页后话"}
	default:
		return []string{"ok"}
	}
}

func streamText(w http.ResponseWriter, segments []string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	for i, seg := range segments {
		if i > 0 {
			time.Sleep(stubSegmentGap)
		}
		writeChunk(w, seg)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
	flush(w)
}

func writeChunk(w http.ResponseWriter, text string) {
	v := map[string]any{"choices": []any{map[string]any{
		"index": 0,
		"delta": map[string]any{"role": "assistant", "content": text},
	}}}
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", raw)
	flush(w)
}

func flush(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
