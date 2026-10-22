//go:build smoke

package smoke

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

	mu     sync.Mutex
	reqs   []StubRequest
	gates  map[string]*gate
	missed int
	busy   *busyScript // Set before serve starts; never used by the six owner journeys.
}

type gate struct {
	entered     chan struct{}
	release     chan struct{}
	enteredOnce sync.Once
	releaseOnce sync.Once

	mu      sync.Mutex
	outcome string
}

// How a call held on a gate ended.
const (
	gateReleased  = "released"
	gateCancelled = "cancelled"
	gateClosed    = "closed"
	gateTimedOut  = "timed out"
)

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

// Missed is how many missed-routine summaries the stub answered.
func (s *Stub) Missed() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.missed
}

// lineWith is the first line of text that carries marker.
func lineWith(text, marker string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, marker) {
			return strings.TrimSpace(line)
		}
	}
	return marker
}

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

// hold blocks until Release, the caller gives up, or the test ends, and records
// how the first call left the gate. Only Release counts as the test letting the
// step finish; the other outcomes are evidence of a cancel, shutdown or hang.
func (s *Stub) hold(ctx context.Context, name string) string {
	g := s.gateFor(name)
	reason := gateTimedOut
	select {
	case <-g.release:
		reason = gateReleased
	case <-ctx.Done():
		reason = gateCancelled
	case <-s.closed:
		reason = gateClosed
	case <-time.After(stubHoldLimit):
		reason = gateTimedOut
	}
	g.mu.Lock()
	if g.outcome == "" {
		g.outcome = reason
	}
	g.mu.Unlock()
	return reason
}

// Outcome says how the first call held on name ended: released, cancelled,
// closed or timed out. Empty means no call has left that gate yet.
func (s *Stub) Outcome(name string) string {
	g := s.gateFor(name)
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.outcome
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

	if s.busy != nil && s.busy.handle(w, r, req, caller, bot, raw) {
		return
	}
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
// gate; only the test's Release may finish it, so a cancelled or restarted
// call cannot produce LONG_RESULT_42 by accident. A task that asks for a
// directory listing and summary.txt runs shell then write_file.
func (s *Stub) subagent(w http.ResponseWriter, r *http.Request, req stubRequest) {
	text := lastUser(req)
	switch {
	case strings.Contains(text, "SMOKE_LONG_TASK"):
		s.Enter("long")
		if reason := s.hold(r.Context(), "long"); reason != gateReleased {
			return
		}
		streamText(w, []string{"LONG_RESULT_42"})
	case strings.Contains(text, "列出文件") && strings.Contains(text, "summary.txt"):
		s.listAndSummarize(w, req, text)
	default:
		streamText(w, []string{"ok"})
	}
}

// listAndSummarize is the dev subagent's script: shell ls, write summary.txt,
// then report where it wrote.
func (s *Stub) listAndSummarize(w http.ResponseWriter, req stubRequest, task string) {
	dir := firstAbsPath(task)
	if dir == "" {
		streamText(w, []string{"ok"})
		return
	}
	sawShell, sawWrite, shellOut := toolProgress(req)
	summary := filepath.Join(dir, "summary.txt")
	switch {
	case !sawShell:
		streamTool(w, "call_ls", "shell", argsJSON(map[string]string{"command": "ls -1 " + shellQuote(dir)}))
	case !sawWrite:
		streamTool(w, "call_write", "write_file", argsJSON(map[string]string{
			"path":    summary,
			"content": "files:\n" + commandOutput(shellOut),
		}))
	default:
		streamText(w, []string{"wrote " + summary})
	}
}

// foreground answers the owner's line. A turn interrupted by a newer owner
// message is re-sent with both lines, so that case looks at the whole request.
func (s *Stub) foreground(w http.ResponseWriter, r *http.Request, req stubRequest) {
	all, text := joined(req), lastUser(req)
	switch {
	case strings.Contains(text, "<routine"):
		if strings.Contains(text, "SMOKE_FIRE_NEWS") {
			if !sawTool(req, "notify_owner") {
				streamTool(w, "call_notify", "notify_owner", argsJSON(map[string]string{"text": "NOTE_NEWS"}))
				return
			}
			streamText(w, []string{"noted"})
			return
		}
		streamText(w, []string{"SILENT"})
	case strings.Contains(text, "Missed routine times"):
		s.mu.Lock()
		s.missed++
		s.mu.Unlock()
		streamText(w, []string{"SILENT"})
	case strings.Contains(text, "B_OPINION_77"):
		streamText(w, []string{"B 的意见是：" + lineWith(text, "B_OPINION_77")})
	case strings.Contains(text, "SMOKE_ASK_B"):
		if !sawTool(req, "message_bot") {
			streamTool(w, "call_ask", "message_bot", argsJSON(map[string]string{
				"bot": "bob", "text": "SMOKE_Q 你怎么看方案 X",
			}))
			return
		}
		streamText(w, []string{"已经问 B 了"})
	case strings.Contains(text, "SMOKE_Q"):
		if !sawTool(req, "message_bot") {
			streamTool(w, "call_reply", "message_bot", argsJSON(map[string]string{
				"bot": "ada", "text": "B_OPINION_77：建议先做 X",
			}))
			return
		}
		streamText(w, []string{"sent"})
	case strings.Contains(text, "SMOKE_LOOP"):
		if !sawTool(req, "message_bot") {
			other := "bob"
			if botName(joined(req)) == "bob" {
				other = "ada"
			}
			streamTool(w, "call_loop", "message_bot", argsJSON(map[string]string{
				"bot": other, "text": "SMOKE_LOOP",
			}))
			return
		}
		streamText(w, []string{"sent"})
	case strings.Contains(text, "SMOKE_ROUTINE"):
		if !sawTool(req, "routine_create") {
			f := strings.Fields(text)
			if len(f) < 3 {
				streamText(w, []string{"bad cue"})
				return
			}
			streamTool(w, "call_routine", "routine_create", argsJSON(map[string]string{
				"name": "smoke-" + f[1], "prompt": "SMOKE_FIRE_" + f[1], "at": f[2],
			}))
			return
		}
		streamText(w, []string{"定好了"})
	case strings.Contains(text, "SMOKE_CRON"):
		if !sawTool(req, "routine_create") {
			f := strings.Fields(text)
			if len(f) < 2 {
				streamText(w, []string{"bad cue"})
				return
			}
			streamTool(w, "call_routine", "routine_create", argsJSON(map[string]string{
				"name": "smoke-" + f[1], "prompt": "SMOKE_FIRE_" + f[1], "cron": "*/5 * * * *",
			}))
			return
		}
		streamText(w, []string{"定好了"})
	case strings.Contains(text, "wrote ") && strings.Contains(text, "summary.txt"):
		streamText(w, []string{"我列出了目录里的文件，写好了 " + summaryPath(text)})
	case strings.Contains(text, "SMOKE_DO"):
		if !sawTool(req, "dispatch") {
			dir := afterMarker(text, "dir=")
			task := "在 " + dir + " 列出文件，写 summary.txt，第一行 files:，后面每行一个文件名"
			streamTool(w, "call_dispatch", "dispatch", argsJSON(map[string]string{"type": "dev", "task": task}))
			return
		}
		streamText(w, []string{"交给子代理了"})
	case strings.Contains(text, "SMOKE_REMEMBER"):
		if !sawTool(req, "write_file") {
			streamTool(w, "call_remember", "write_file", argsJSON(map[string]string{
				"path":    memoryPath(req, "Person"),
				"content": rememberLine,
			}))
			return
		}
		streamText(w, []string{"记住了"})
	case strings.Contains(text, "SMOKE_CORRECT"):
		// The file already exists, so the kernel asks for a read before the
		// write; the stub reads it and then rewrites both lines.
		switch {
		case !sawTool(req, "read_file") && !sawTool(req, "write_file"):
			streamTool(w, "call_read", "read_file", argsJSON(map[string]string{
				"path": memoryPath(req, "Person"),
			}))
		case !sawTool(req, "write_file"):
			streamTool(w, "call_correct", "write_file", argsJSON(map[string]string{
				"path":    memoryPath(req, "Person"),
				"content": rememberLine + "- (2026-10-20) 主人已不再对花生过敏 MARK_PEANUT_2\n",
			}))
		default:
			streamText(w, []string{"改好了"})
		}
	case strings.Contains(text, "SMOKE_RECALL"):
		streamText(w, []string{peanutLines(req)})
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

// toolProgress is the subagent's step count and the shell output it needs for
// the write. Only tool messages between the shell call and the write count.
func toolProgress(req stubRequest) (sawShell, sawWrite bool, shellOut string) {
	for _, m := range req.Messages {
		for _, raw := range m.ToolCalls {
			var tc struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			}
			if json.Unmarshal(raw, &tc) != nil {
				continue
			}
			switch tc.Function.Name {
			case "shell":
				sawShell = true
			case "write_file":
				sawWrite = true
			}
		}
		if m.Role == "tool" && sawShell && !sawWrite {
			shellOut = m.Content
		}
	}
	return sawShell, sawWrite, shellOut
}

// commandOutput turns the shell tool's result into the command's own output.
// The tool prepends "exit N", adds a "cwd: ..." line and trims the trailing
// newline; the listing keeps its own line ending so the file matches ls -1.
func commandOutput(out string) string {
	var kept []string
	for i, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "cwd: ") {
			continue
		}
		if i == 0 && exitLine.MatchString(line) {
			continue
		}
		kept = append(kept, line)
	}
	s := strings.Join(kept, "\n")
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s
}

var exitLine = regexp.MustCompile(`^exit \d+$`)

// firstAbsPath is the first absolute path in the task text.
func firstAbsPath(text string) string {
	m := regexp.MustCompile(`(?:^|[\s，])(/[^\s，]+)`).FindStringSubmatch(text)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

// summaryPath is the absolute summary.txt path in a subagent report.
func summaryPath(text string) string {
	m := regexp.MustCompile(`(/[^\s，]*/summary\.txt)`).FindStringSubmatch(text)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

// afterMarker is the text after mark, up to the next space.
func afterMarker(text, mark string) string {
	i := strings.Index(text, mark)
	if i < 0 {
		return ""
	}
	fields := strings.Fields(text[i+len(mark):])
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func argsJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// rememberLine is one memory fact line the stub writes to the person file.
const rememberLine = "- (2026-10-20) 主人对花生过敏 MARK_PEANUT_1\n"

// memoryPath is the memory file named by the snapshot heading
// "### <Scope> — <path>; ...".
func memoryPath(req stubRequest, scope string) string {
	prefix := "### " + scope + " — "
	for _, m := range req.Messages {
		for _, line := range strings.Split(m.Content, "\n") {
			if strings.HasPrefix(line, prefix) {
				return strings.SplitN(strings.TrimPrefix(line, prefix), ";", 2)[0]
			}
		}
	}
	return ""
}

// peanutLines lists every request line that carries a marker, as a model
// reading its context would; no marker means it does not know.
func peanutLines(req stubRequest) string {
	var lines []string
	add := func(text string) {
		for _, line := range strings.Split(text, "\n") {
			if strings.Contains(line, "MARK_PEANUT_") {
				lines = append(lines, strings.TrimSpace(line))
			}
		}
	}
	for _, m := range req.Messages {
		add(m.Content)
		// The model also sees the arguments of its own tool calls.
		for _, raw := range m.ToolCalls {
			var tc struct {
				Function struct {
					Arguments string `json:"arguments"`
				} `json:"function"`
			}
			if json.Unmarshal(raw, &tc) == nil {
				add(tc.Function.Arguments)
			}
		}
	}
	if len(lines) == 0 {
		return "不知道"
	}
	return strings.Join(lines, "\n")
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

// streamHold sends one piece, then waits on the gate and records why the call
// left it. A cancelled request ends the wait and nothing more is written; the
// kernel re-sends the turn.
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
