//go:build smoke

package smoke

import (
	"context"
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
	// A held call must not keep the test server alive if a bug stops the
	// cancel; the cleanup closes closed and the wait returns.
	stubHoldLimit = 90 * time.Second
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
	srv    *httptest.Server
	closed chan struct{}

	mu    sync.Mutex
	reqs  []StubRequest
	gates map[string]*gate
}

type gate struct {
	entered     chan struct{}
	release     chan struct{}
	enteredOnce sync.Once
	releaseOnce sync.Once
}

// StartStub serves /v1/chat/completions until the test ends.
func StartStub(t *testing.T) *Stub {
	t.Helper()
	s := &Stub{closed: make(chan struct{}), gates: map[string]*gate{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", s.handle)
	s.srv = httptest.NewServer(mux)
	t.Cleanup(func() {
		close(s.closed)
		s.srv.Close()
	})
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

// Enter marks that a held step has started.
func (s *Stub) Enter(name string) {
	g := s.gateFor(name)
	g.enteredOnce.Do(func() { close(g.entered) })
}

// Entered reports whether Enter(name) has run.
func (s *Stub) Entered(name string) bool {
	g := s.gateFor(name)
	select {
	case <-g.entered:
		return true
	default:
		return false
	}
}

// Release lets a held step answer.
func (s *Stub) Release(name string) {
	g := s.gateFor(name)
	g.releaseOnce.Do(func() { close(g.release) })
}

func (s *Stub) gateFor(name string) *gate {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.gates[name]
	if !ok {
		g = &gate{entered: make(chan struct{}), release: make(chan struct{})}
		s.gates[name] = g
	}
	return g
}

// hold blocks until Release, the caller gives up, or the test ends.
func (s *Stub) hold(ctx context.Context, name string) {
	g := s.gateFor(name)
	select {
	case <-g.release:
	case <-ctx.Done():
	case <-s.closed:
	case <-time.After(stubHoldLimit):
	}
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
		s.subagent(w, r, req)
	default:
		s.foreground(w, r, req)
	}
}

// subagent answers the task it was given. SMOKE_LONG_TASK parks on the long
// gate until the test lets it finish.
func (s *Stub) subagent(w http.ResponseWriter, r *http.Request, req stubRequest) {
	if strings.Contains(lastUser(req), "SMOKE_LONG_TASK") {
		s.Enter("long")
		s.hold(r.Context(), "long")
		streamText(w, []string{"LONG_RESULT_42"})
		return
	}
	streamText(w, []string{"ok"})
}

// foreground answers the owner's line. A turn interrupted by a newer owner
// message is re-sent with both lines, so that case looks at the whole request.
func (s *Stub) foreground(w http.ResponseWriter, r *http.Request, req stubRequest) {
	all, text := joined(req), lastUser(req)
	switch {
	case strings.Contains(all, "SMOKE_SLOW") && strings.Contains(all, "SMOKE_ASIDE_2"):
		streamText(w, []string{"先答插话"})
	case strings.Contains(text, "SMOKE_SLOW"):
		// The first piece goes out before the hold, as a real model would.
		streamHold(w, r.Context(), s, "fg", "稍等，")
	case strings.Contains(text, "SMOKE_ASIDE_2"):
		streamText(w, []string{"ok"})
	case strings.Contains(text, "SMOKE_ASIDE"):
		streamText(w, []string{"插话的回答"})
	case strings.Contains(text, "LONG_RESULT_42"):
		streamText(w, []string{"长任务完成：LONG_RESULT_42"})
	case strings.Contains(text, "SMOKE_LONG") && !sawTool(req, "dispatch"):
		streamTool(w, "call_dispatch", "dispatch", `{"type":"general","task":"SMOKE_LONG_TASK"}`)
	case strings.Contains(text, "SMOKE_LONG"):
		streamText(w, []string{"已交给后台"})
	case helloCall.MatchString(text):
		n := helloCall.FindStringSubmatch(text)[1]
		streamText(w, []string{"你好，", "这是第 " + n + " 次", "流式回复"})
	case strings.Contains(text, "SMOKE_FROM_TERMINAL"):
		streamText(w, []string{"终端你好"})
	case strings.Contains(text, "SMOKE_FROM_WEB_LATE"):
		streamText(w, []string{"网页后话"})
	default:
		streamText(w, []string{"ok"})
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

// lastUser is the newest user line. In a turn that took in a newer owner
// message this is the kernel's internal note, so scripts that span both lines
// look at the whole request instead.
func lastUser(req stubRequest) string {
	out := ""
	for _, m := range req.Messages {
		if m.Role == "user" {
			out = m.Content
		}
	}
	return out
}

// sawTool reports whether the model already called name after the newest user
// line, which is how a scripted tool step counts its progress.
func sawTool(req stubRequest, name string) bool {
	saw := false
	for _, m := range req.Messages {
		if m.Role == "user" {
			saw = false
		}
		for _, raw := range m.ToolCalls {
			var tc struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			}
			if json.Unmarshal(raw, &tc) == nil && tc.Function.Name == name {
				saw = true
			}
		}
	}
	return saw
}

// startSSE writes the headers of a streaming answer.
func startSSE(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
}

func streamText(w http.ResponseWriter, segments []string) {
	startSSE(w)
	for i, seg := range segments {
		if i > 0 {
			time.Sleep(stubSegmentGap)
		}
		writeChunk(w, seg)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
	flush(w)
}

// streamHold sends one piece, then waits on the gate. A cancelled request
// ends the wait and nothing more is written; the kernel re-sends the turn.
func streamHold(w http.ResponseWriter, ctx context.Context, s *Stub, name, first string) {
	startSSE(w)
	writeChunk(w, first)
	s.Enter(name)
	s.hold(ctx, name)
}

func streamTool(w http.ResponseWriter, id, name, args string) {
	startSSE(w)
	writeData(w, map[string]any{"choices": []any{map[string]any{
		"index": 0,
		"delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{
			"index": 0,
			"id":    id,
			"type":  "function",
			"function": map[string]any{
				"name":      name,
				"arguments": args,
			},
		}}},
	}}})
	fmt.Fprint(w, "data: [DONE]\n\n")
	flush(w)
}

func writeChunk(w http.ResponseWriter, text string) {
	writeData(w, map[string]any{"choices": []any{map[string]any{
		"index": 0,
		"delta": map[string]any{"role": "assistant", "content": text},
	}}})
}

func writeData(w http.ResponseWriter, v any) {
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
