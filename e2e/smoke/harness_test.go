//go:build smoke

// Package smoke runs the owner journeys against a real xbot process.
//
// TestMain builds the binary once. Each test starts `xbot serve` on a private
// data root, logs in with the printed token, and talks to it over the real
// HTTP API; only the model is a scripted stub (see stub_test.go).
package smoke

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const (
	startTimeout = 15 * time.Second
	stopTimeout  = 10 * time.Second
	sendTimeout  = 60 * time.Second

	// servePassInterval mirrors the default --interval of xbot serve, which
	// the harness leaves alone. An inbox event with no direct wake waits at
	// most this long for the next pass.
	servePassInterval = 15 * time.Second

	listenPrefix     = "listening on "
	loginTokenPrefix = "登录码（login token）: "
)

// xbotBinary is built once by TestMain and shared by every test.
var xbotBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "xbot-smoke-bin-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "smoke: temp dir:", err)
		os.Exit(1)
	}
	xbotBinary = filepath.Join(dir, "xbot")
	cmd := exec.Command("go", "build", "-o", xbotBinary, "xbot/cmd/xbot")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "smoke: go build: %v\n%s", err, out)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// envWith returns base with the given KEY=VALUE pairs replacing any the
// process already has, so a duplicate does not leave the real HOME in place.
func envWith(base []string, pairs ...string) []string {
	replace := map[string]bool{}
	for i := 0; i+1 < len(pairs); i += 2 {
		replace[pairs[i]] = true
	}
	out := make([]string, 0, len(base)+len(pairs)/2)
	for _, e := range base {
		k, _, _ := strings.Cut(e, "=")
		if replace[k] {
			continue
		}
		out = append(out, e)
	}
	return append(out, pairs...)
}

// Server is one `xbot serve` process with its own data root.
type Server struct {
	t     *testing.T
	Root  string
	URL   string
	Token string

	env       []string
	clockFile string
	clock     time.Duration
	cmd       *exec.Cmd
	client    *http.Client
	exited    chan struct{}
	waitErr   error

	mu     sync.Mutex
	stderr strings.Builder
}

// Start builds a private data root, starts `xbot serve` on a free port, and
// logs in. The model stub is reached through XBOT_BASE_URL.
func Start(t *testing.T, stub *Stub) *Server {
	t.Helper()
	if xbotBinary == "" {
		t.Fatalf("smoke: TestMain did not build the xbot binary")
	}
	base := t.TempDir()
	s := &Server{
		t:      t,
		Root:   filepath.Join(base, "data"),
		exited: make(chan struct{}),
	}
	t.Logf("data root: %s", s.Root)
	// Every server reads its clock shift from this file; zero is real time,
	// so tests that do not move it see the normal clock.
	s.clockFile = filepath.Join(base, "clock")
	if err := os.WriteFile(s.clockFile, []byte("0"), 0600); err != nil {
		t.Fatalf("clock file: %v", err)
	}
	s.env = envWith(os.Environ(),
		"HOME="+base,
		"XBOT_BASE_URL="+stub.BaseURL(),
		"XBOT_API_KEY=smoke",
		"XBOT_MODEL=smoke",
		"XBOT_AUTO_UPGRADE=0",
		"XBOT_FAKE_CLOCK_FILE="+s.clockFile,
	)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	s.client = &http.Client{Jar: jar}
	t.Cleanup(s.Stop)
	s.launch()
	s.Login()
	return s
}

// launch starts one serve process on s.Root and waits for its two startup
// lines. Start calls it once; Restart calls it again on the same data root.
func (s *Server) launch() {
	s.t.Helper()
	s.exited = make(chan struct{})
	s.mu.Lock()
	s.stderr.Reset()
	s.waitErr = nil
	s.mu.Unlock()

	s.cmd = exec.Command(xbotBinary,
		"--root", s.Root, "serve",
		"--http", "127.0.0.1:0",
		"--no-auto-upgrade",
	)
	s.cmd.Env = s.env
	s.cmd.Stdout = io.Discard
	stderr, err := s.cmd.StderrPipe()
	if err != nil {
		s.t.Fatalf("stderr pipe: %v", err)
	}
	if err := s.cmd.Start(); err != nil {
		s.t.Fatalf("start xbot serve: %v", err)
	}
	go func() {
		s.waitErr = s.cmd.Wait()
		close(s.exited)
	}()

	listen := make(chan string, 1)
	token := make(chan string, 1)
	go s.scanStderr(stderr, listen, token)

	s.URL = s.await("listening", listen)
	s.Token = s.await("login token", token)
}

// Restart stops serve, waits for it to exit, and starts it again on the same
// data root, then logs in again.
func (s *Server) Restart() {
	s.t.Helper()
	s.Stop()
	s.launch()
	s.Login()
}

// Advance moves this server's clock forward by d. serve rereads the offset
// file on every clock call, so the change applies to the running process.
func (s *Server) Advance(d time.Duration) {
	s.t.Helper()
	s.clock += d
	raw := []byte(strconv.FormatInt(int64(s.clock/time.Second), 10))
	// Replace the file atomically: serve rereads it on every clock call, and a
	// reader must never see a half-written offset.
	tmp := s.clockFile + ".new"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		s.t.Fatalf("clock file: %v", err)
	}
	if err := os.Rename(tmp, s.clockFile); err != nil {
		s.t.Fatalf("clock file: %v", err)
	}
}

// PatchBot changes profile fields with PATCH /api/bots/{name}.
func (s *Server) PatchBot(bot string, fields map[string]any) {
	s.t.Helper()
	status, body := s.do(http.MethodPatch, "/api/bots/"+url.PathEscape(bot), fields)
	if status != http.StatusOK {
		s.t.Fatalf("patch %s: %d %s", bot, status, body)
	}
}

func (s *Server) scanStderr(r io.Reader, listen, token chan<- string) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		s.mu.Lock()
		s.stderr.WriteString(line)
		s.stderr.WriteByte('\n')
		s.mu.Unlock()
		switch {
		case strings.HasPrefix(line, listenPrefix):
			select {
			case listen <- strings.TrimSpace(strings.TrimPrefix(line, listenPrefix)):
			default:
			}
		case strings.HasPrefix(line, loginTokenPrefix):
			select {
			case token <- strings.TrimSpace(strings.TrimPrefix(line, loginTokenPrefix)):
			default:
			}
		}
	}
}

func (s *Server) await(what string, ch <-chan string) string {
	select {
	case v := <-ch:
		return v
	case <-s.exited:
		s.t.Fatalf("xbot serve exited before printing the %s line: %v\nstderr:\n%s", what, s.waitErr, s.Stderr())
	case <-time.After(startTimeout):
		s.t.Fatalf("xbot serve: no %s line within %s\nstderr:\n%s", what, startTimeout, s.Stderr())
	}
	return ""
}

// Stderr returns everything serve has written to stderr so far.
func (s *Server) Stderr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stderr.String()
}

// Login posts the printed token and checks that the bot list answers with the
// session cookie.
func (s *Server) Login() {
	status, body := s.do(http.MethodPost, "/api/login", map[string]string{"token": s.Token})
	if status != http.StatusOK {
		s.t.Fatalf("login: %d %s", status, body)
	}
	status, body = s.do(http.MethodGet, "/api/bots", nil)
	if status != http.StatusOK {
		s.t.Fatalf("GET /api/bots: %d %s", status, body)
	}
}

// CreateBot creates a bot whose persona carries BOTNAME=<name> so the stub can
// tell the bots apart.
func (s *Server) CreateBot(name, persona string) {
	s.t.Helper()
	if !strings.Contains(persona, "BOTNAME=") {
		persona = strings.TrimSpace(persona) + "\n\nBOTNAME=" + name
	}
	status, body := s.do(http.MethodPost, "/api/bots", map[string]string{"name": name, "persona": persona})
	if status != http.StatusOK {
		s.t.Fatalf("create bot %s: %d %s", name, status, body)
	}
}

func (s *Server) do(method, path string, in any) (int, []byte) {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			s.t.Fatalf("%s %s: %v", method, path, err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.URL+path, body)
	if err != nil {
		s.t.Fatalf("%s %s: %v", method, path, err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		s.t.Fatalf("%s %s: %v\nstderr:\n%s", method, path, err, s.Stderr())
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		s.t.Fatalf("%s %s: read body: %v", method, path, err)
	}
	return resp.StatusCode, raw
}

// Delta is one streamed piece of a reply, with the time it reached the client.
type Delta struct {
	At   time.Time
	Text string
}

// ToolEvent is one tool event from the stream.
type ToolEvent struct {
	Name    string
	Summary string
}

// Reply is one POST /api/bots/{name}/messages request and its SSE stream.
type Reply struct {
	Sent      time.Time
	Deltas    []Delta
	Tools     []ToolEvent
	Done      string
	HandedOff bool
	Error     string
	Status    int
}

// Stream is one message request still in flight.
type Stream struct {
	s    *Server
	bot  string
	text string

	reply     *Reply
	done      chan struct{}
	failure   string
	approvals int
	sawDone   bool
}

// BeginSend posts a message and reads its stream in the background, for a
// test that must keep talking while the turn is still running.
func (s *Server) BeginSend(bot, text string) *Stream {
	st := &Stream{
		s:     s,
		bot:   bot,
		text:  text,
		reply: &Reply{Sent: time.Now()},
		done:  make(chan struct{}),
	}
	go st.run()
	return st
}

// Wait blocks until the stream ends and fails on anything but a complete,
// error-free answer.
func (st *Stream) Wait() *Reply {
	<-st.done
	s := st.s
	if st.failure != "" {
		s.t.Fatalf("%s", st.failure)
	}
	if st.approvals > 0 {
		s.t.Fatalf("message %q raised %d approval event(s); the smoke script has none", st.text, st.approvals)
	}
	if st.reply.Error != "" {
		s.t.Fatalf("message %q: stream error: %s", st.text, st.reply.Error)
	}
	if !st.sawDone {
		s.t.Fatalf("message %q: stream ended without a done event", st.text)
	}
	return st.reply
}

// Send posts a message and waits for the whole event stream.
func (s *Server) Send(bot, text string) *Reply {
	s.t.Helper()
	return s.BeginSend(bot, text).Wait()
}

func (st *Stream) run() {
	defer close(st.done)
	s := st.s
	raw, err := json.Marshal(map[string]string{"text": st.text})
	if err != nil {
		st.failure = fmt.Sprintf("marshal message: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.URL+"/api/bots/"+url.PathEscape(st.bot)+"/messages", bytes.NewReader(raw))
	if err != nil {
		st.failure = fmt.Sprintf("POST message %q: %v", st.text, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := s.client.Do(req)
	if err != nil {
		st.failure = fmt.Sprintf("POST message %q: %v\nstderr:\n%s", st.text, err, s.Stderr())
		return
	}
	defer resp.Body.Close()
	st.reply.Status = resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		st.failure = fmt.Sprintf("POST message %q: %d %s", st.text, resp.StatusCode, body)
		return
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	var event string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			event = ""
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event: "))
		case strings.HasPrefix(line, "data: "):
			data := strings.TrimPrefix(line, "data: ")
			switch event {
			case "delta":
				var d struct {
					Text string `json:"text"`
				}
				if err := json.Unmarshal([]byte(data), &d); err == nil {
					st.reply.Deltas = append(st.reply.Deltas, Delta{At: time.Now(), Text: d.Text})
				}
			case "tool":
				var d struct {
					Name    string `json:"name"`
					Summary string `json:"summary"`
				}
				if err := json.Unmarshal([]byte(data), &d); err == nil {
					st.reply.Tools = append(st.reply.Tools, ToolEvent{Name: d.Name, Summary: d.Summary})
				}
			case "approval":
				st.approvals++
			case "done":
				var d struct {
					Text      string `json:"text"`
					HandedOff bool   `json:"handed_off"`
				}
				if err := json.Unmarshal([]byte(data), &d); err == nil {
					st.reply.Done = d.Text
					st.reply.HandedOff = d.HandedOff
				}
				st.sawDone = true
			case "error":
				var d struct {
					Error string `json:"error"`
				}
				if err := json.Unmarshal([]byte(data), &d); err == nil {
					st.reply.Error = d.Error
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		st.failure = fmt.Sprintf("read stream for %q: %v", st.text, err)
	}
}

// Row is one transcript line as GET /api/bots/{name}/transcript returns it.
type Row struct {
	Seq       int           `json:"seq"`
	Role      string        `json:"role"`
	Content   string        `json:"content"`
	Name      string        `json:"name"`
	Time      string        `json:"time"`
	ToolCalls []RowToolCall `json:"tool_calls,omitempty"`
	ReplyTo   *int          `json:"reply_to,omitempty"`
	Branch    string        `json:"branch,omitempty"`
	Kind      string        `json:"kind,omitempty"`
	From      string        `json:"from,omitempty"`
	Task      string        `json:"task,omitempty"`
	Status    string        `json:"status,omitempty"`
	Steps     int           `json:"steps,omitempty"`
	Internal  bool          `json:"internal,omitempty"`
}

// RowToolCall is one tool call on an assistant row.
type RowToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// Transcript reads the append-only transcript.
func (s *Server) Transcript(bot string) []Row {
	s.t.Helper()
	status, body := s.do(http.MethodGet, "/api/bots/"+url.PathEscape(bot)+"/transcript", nil)
	if status != http.StatusOK {
		s.t.Fatalf("transcript of %s: %d %s", bot, status, body)
	}
	var out struct {
		Messages []Row `json:"messages"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		s.t.Fatalf("transcript of %s: %v", bot, err)
	}
	if out.Messages == nil {
		out.Messages = []Row{}
	}
	return out.Messages
}

// Agent is one subagent record from GET /api/bots/{name}.
type Agent struct {
	Notable []struct {
		Kind string `json:"kind"`
		Text string `json:"text"`
	} `json:"notable"`
	ID     string `json:"id"`
	Status string `json:"status"`
	Task   string `json:"task"`
	Mode   string `json:"mode"`
	Steps  int    `json:"steps"`
}

// Agents reads the panel's subagent list, the data the 任务 panel draws.
func (s *Server) Agents(bot string) []Agent {
	s.t.Helper()
	status, body := s.do(http.MethodGet, "/api/bots/"+url.PathEscape(bot), nil)
	if status != http.StatusOK {
		s.t.Fatalf("panel of %s: %d %s", bot, status, body)
	}
	var out struct {
		Agents []Agent `json:"agents"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		s.t.Fatalf("panel of %s: %v", bot, err)
	}
	if out.Agents == nil {
		out.Agents = []Agent{}
	}
	return out.Agents
}

// State reads the bot's state from GET /api/bots/status: idle, running,
// awaiting or offline. An empty string means the bot is not listed.
func (s *Server) State(bot string) string {
	s.t.Helper()
	status, body := s.do(http.MethodGet, "/api/bots/status", nil)
	if status != http.StatusOK {
		s.t.Fatalf("bot status: %d %s", status, body)
	}
	var out struct {
		Bots []struct {
			Bot   string `json:"bot"`
			State string `json:"state"`
		} `json:"bots"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		s.t.Fatalf("bot status: %v", err)
	}
	for _, b := range out.Bots {
		if b.Bot == bot {
			return b.State
		}
	}
	return ""
}

// MemoryFact is one entry from the panel's memory list.
type MemoryFact struct {
	Scope string `json:"scope"`
	Text  string `json:"text"`
}

// Memory reads the panel's memory list, what 详情的「记忆」shows.
func (s *Server) Memory(bot string) []MemoryFact {
	s.t.Helper()
	status, body := s.do(http.MethodGet, "/api/bots/"+url.PathEscape(bot), nil)
	if status != http.StatusOK {
		s.t.Fatalf("panel of %s: %d %s", bot, status, body)
	}
	var out struct {
		Memory []MemoryFact `json:"memory"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		s.t.Fatalf("panel of %s: %v", bot, err)
	}
	if out.Memory == nil {
		out.Memory = []MemoryFact{}
	}
	return out.Memory
}

// PeerMessage is one direct message between two bots.
type PeerMessage struct {
	ID   string `json:"id"`
	Seq  int    `json:"seq"`
	From string `json:"from"`
	To   string `json:"to"`
	Text string `json:"text"`
}

// PeerMessages reads the private thread between two local bots.
func (s *Server) PeerMessages(a, b string) []PeerMessage {
	s.t.Helper()
	status, body := s.do(http.MethodGet, "/api/bots/"+url.PathEscape(a)+"/peers/"+url.PathEscape(b)+"/messages", nil)
	if status != http.StatusOK {
		s.t.Fatalf("peer messages %s/%s: %d %s", a, b, status, body)
	}
	var out struct {
		Messages []PeerMessage `json:"messages"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		s.t.Fatalf("peer messages %s/%s: %v", a, b, err)
	}
	if out.Messages == nil {
		out.Messages = []PeerMessage{}
	}
	return out.Messages
}

// Note is one unread note from GET /api/notes.
type Note struct {
	ID   string `json:"id"`
	Bot  string `json:"bot"`
	Text string `json:"text"`
	Time string `json:"time"`
}

// Notes reads the unread notes; the sidebar avatar of that bot shows them as an
// unread dot.
func (s *Server) Notes() []Note {
	s.t.Helper()
	status, body := s.do(http.MethodGet, "/api/notes", nil)
	if status != http.StatusOK {
		s.t.Fatalf("notes: %d %s", status, body)
	}
	var out struct {
		Notes []Note `json:"notes"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		s.t.Fatalf("notes: %v", err)
	}
	if out.Notes == nil {
		out.Notes = []Note{}
	}
	return out.Notes
}

// WaitFor polls cond until it is true. On timeout it reports what cond last saw.
func WaitFor(t *testing.T, d time.Duration, what string, cond func() (bool, string)) {
	t.Helper()
	deadline := time.Now().Add(d)
	var last string
	for {
		ok, snapshot := cond()
		if ok {
			return
		}
		if snapshot != "" {
			last = snapshot
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s\nlast seen:\n%s", d, what, last)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Chat is one `xbot chat <bot>` process.
type Chat struct {
	t       *testing.T
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  lockedBuffer
	stderr  lockedBuffer
	exited  chan struct{}
	waitErr error
}

type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// Chat opens the terminal on the same bot, in the same data root.
func (s *Server) Chat(bot string) *Chat {
	s.t.Helper()
	c := &Chat{t: s.t, exited: make(chan struct{})}
	c.cmd = exec.Command(xbotBinary, "--root", s.Root, "chat", bot)
	c.cmd.Env = s.env
	stdin, err := c.cmd.StdinPipe()
	if err != nil {
		s.t.Fatalf("chat stdin: %v", err)
	}
	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		s.t.Fatalf("chat stdout: %v", err)
	}
	stderr, err := c.cmd.StderrPipe()
	if err != nil {
		s.t.Fatalf("chat stderr: %v", err)
	}
	if err := c.cmd.Start(); err != nil {
		s.t.Fatalf("start xbot chat: %v", err)
	}
	c.stdin = stdin
	go func() { _, _ = io.Copy(&c.stdout, stdout) }()
	go func() { _, _ = io.Copy(&c.stderr, stderr) }()
	go func() {
		c.waitErr = c.cmd.Wait()
		close(c.exited)
	}()
	s.t.Cleanup(c.Stop)
	return c
}

// Stdout is everything the terminal printed so far.
func (c *Chat) Stdout() string { return c.stdout.String() }

// Stderr is everything the terminal printed on stderr so far (prompts, tools).
func (c *Chat) Stderr() string { return c.stderr.String() }

// Say writes one line, as the owner would type it.
func (c *Chat) Say(text string) {
	c.t.Helper()
	if _, err := fmt.Fprintln(c.stdin, text); err != nil {
		c.t.Fatalf("chat: say %q: %v\nstdout:\n%s\nstderr:\n%s", text, err, c.Stdout(), c.Stderr())
	}
}

// Quit ends the terminal with the documented exit word and requires a clean
// exit. A terminal that died before quit, or crashed on the way out, is a
// failure here rather than a pass.
func (c *Chat) Quit() {
	c.t.Helper()
	select {
	case <-c.exited:
		c.t.Fatalf("xbot chat exited before quit: %v\nstdout:\n%s\nstderr:\n%s", c.waitErr, c.Stdout(), c.Stderr())
	default:
	}
	if _, err := fmt.Fprintln(c.stdin, "exit"); err != nil {
		c.t.Fatalf("chat: write exit: %v\nstdout:\n%s\nstderr:\n%s", err, c.Stdout(), c.Stderr())
	}
	_ = c.stdin.Close()
	select {
	case <-c.exited:
		if c.waitErr != nil {
			c.t.Fatalf("xbot chat exited with %v after exit\nstdout:\n%s\nstderr:\n%s", c.waitErr, c.Stdout(), c.Stderr())
		}
	case <-time.After(stopTimeout):
		_ = c.cmd.Process.Kill()
		<-c.exited
		c.t.Fatalf("xbot chat did not exit after exit\nstdout:\n%s\nstderr:\n%s", c.Stdout(), c.Stderr())
	}
}

// Stop ends the terminal if it is still running.
func (c *Chat) Stop() {
	select {
	case <-c.exited:
		return
	default:
	}
	_ = c.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-c.exited:
	case <-time.After(stopTimeout):
		_ = c.cmd.Process.Kill()
		<-c.exited
	}
}

// Stop sends SIGTERM and waits; a process still up after 10s is killed and the
// test fails.
func (s *Server) Stop() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	select {
	case <-s.exited:
		return
	default:
	}
	_ = s.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-s.exited:
	case <-time.After(stopTimeout):
		_ = s.cmd.Process.Kill()
		<-s.exited
		s.t.Errorf("xbot serve did not stop within %s; killed\nstderr:\n%s", stopTimeout, s.Stderr())
	}
}
