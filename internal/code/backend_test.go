package code

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/provider"
)

func TestLocalModelStepCapIsTheTasks(t *testing.T) {
	l, work := testLink(t)
	p := &countingProvider{}
	l.UseProvider(p)
	out := mustStart(t, l, work, 3, 30, "keep going")
	ev := waitCode(t, l, 1)
	if ev[0].Type != StatusFailed || !strings.Contains(ev[0].Content, "max steps reached") {
		t.Fatalf("%+v", ev[0])
	}
	if p.calls() != 3 {
		t.Fatalf("calls %d", p.calls())
	}
	waitIdle(t, l)
	if !strings.HasPrefix(out, "running ") {
		t.Fatal(out)
	}
}

func TestLocalModelDoneSteerAndCancel(t *testing.T) {
	l, work := testLink(t)
	p := &scriptProvider{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: provider.TextContent("patched the parser")},
	}}
	l.UseProvider(p)
	mustStart(t, l, work, 4, 30, "fix the parser")
	ev := waitCode(t, l, 1)
	if ev[0].Type != StatusDone || !strings.Contains(ev[0].Content, "patched the parser") {
		t.Fatalf("%+v", ev[0])
	}

	l2, work2 := testLink(t)
	hold := newHold()
	l2.UseProvider(hold)
	id := strings.TrimPrefix(mustStart(t, l2, work2, 4, 30, "wait"), "running ")
	<-hold.entered
	note, _ := json.Marshal(map[string]string{"id": id, "text": "also the makefile"})
	if _, err := toolNamed(l2, "code_steer").Run(context.Background(), note); err != nil {
		t.Fatal(err)
	}
	close(hold.release)
	ev = waitCode(t, l2, 1)
	if ev[0].Type != StatusDone || !strings.Contains(hold.second(), "also the makefile") {
		t.Fatalf("event %+v\n%s", ev[0], hold.second())
	}

	l3, work3 := testLink(t)
	block := &blockProvider{entered: make(chan struct{})}
	l3.UseProvider(block)
	id = strings.TrimPrefix(mustStart(t, l3, work3, 4, 30, "block"), "running ")
	<-block.entered
	cancel, _ := json.Marshal(map[string]string{"id": id})
	if _, err := toolNamed(l3, "code_cancel").Run(context.Background(), cancel); err != nil {
		t.Fatal(err)
	}
	ev = waitCode(t, l3, 1)
	if len(ev) != 1 || ev[0].Type != StatusCancelled || ev[0].Sender != id {
		t.Fatalf("%+v", ev)
	}
	waitIdle(t, l)
	waitIdle(t, l2)
	waitIdle(t, l3)
}

func TestCommandAndRunnerBackends(t *testing.T) {
	l, work := testLink(t)
	script := filepath.Join(t.TempDir(), "agent.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'shipped %s' \"$1\" | tee out.txt\n"), 0755); err != nil {
		t.Fatal(err)
	}
	l.UseCommand(script)
	mustStart(t, l, work, 2, 10, "the widget")
	ev := waitCode(t, l, 1)
	if ev[0].Type != StatusDone || !strings.Contains(ev[0].Content, "shipped the widget") {
		t.Fatalf("%+v", ev[0])
	}
	raw, err := os.ReadFile(filepath.Join(work, "out.txt"))
	if err != nil || string(raw) != "shipped the widget" {
		t.Fatalf("%s %v", raw, err)
	}

	svc := NewService()
	svc.Provider = &scriptProvider{resps: []provider.Message{
		{Role: provider.RoleAssistant, Content: provider.TextContent("from the runner")},
	}}
	ui := newCodeServer(t, svc)
	l2, work2 := testLink(t)
	l2.UseRunner(ui, "tok")
	mustStart(t, l2, work2, 2, 10, "remote goal")
	ev = waitCode(t, l2, 1)
	if ev[0].Type != StatusDone || !strings.Contains(ev[0].Content, "from the runner") {
		t.Fatalf("%+v", ev[0])
	}
	waitIdle(t, l)
	waitIdle(t, l2)
}

func TestCheckoutLocalRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	src := t.TempDir()
	init := exec.Command("git", "init")
	init.Dir = src
	if err := init.Run(); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "co")
	if err := checkout(context.Background(), src, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git")); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(t.TempDir(), "plain")
	if err := checkout(context.Background(), "", empty); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(empty, ".git")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

type scriptProvider struct {
	mu    sync.Mutex
	resps []provider.Message
	i     int
	reqs  []provider.Request
}

func (s *scriptProvider) Chat(_ context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = append(s.reqs, req)
	if s.i >= len(s.resps) {
		return provider.Message{}, errors.New("no more responses")
	}
	m := s.resps[s.i]
	s.i++
	return m, nil
}

func (s *scriptProvider) second() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.reqs) < 2 {
		return ""
	}
	var b strings.Builder
	for _, m := range s.reqs[1].Messages {
		b.WriteString(m.Text())
		b.WriteByte('\n')
	}
	return b.String()
}

type countingProvider struct {
	mu sync.Mutex
	n  int
}

func (p *countingProvider) Chat(_ context.Context, _ provider.Request, _ func(string)) (provider.Message, error) {
	p.mu.Lock()
	p.n++
	p.mu.Unlock()
	args, _ := json.Marshal(map[string]string{"path": "missing.txt"})
	return provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
		ID: "c", Type: "function", Function: provider.FunctionCall{Name: "read_file", Arguments: string(args)},
	}})}, nil
}

func (p *countingProvider) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.n
}

func newHold() *holdProvider {
	return &holdProvider{entered: make(chan struct{}), release: make(chan struct{})}
}

type holdProvider struct {
	once    sync.Once
	entered chan struct{}
	release chan struct{}
	mu      sync.Mutex
	reqs    []provider.Request
	n       int
}

func (h *holdProvider) Chat(_ context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
	h.mu.Lock()
	h.n++
	n := h.n
	h.reqs = append(h.reqs, req)
	h.mu.Unlock()
	if n == 1 {
		h.once.Do(func() { close(h.entered) })
		<-h.release
		args, _ := json.Marshal(map[string]string{"path": "missing.txt"})
		return provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{
			ID: "c", Type: "function", Function: provider.FunctionCall{Name: "read_file", Arguments: string(args)},
		}})}, nil
	}
	return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("steered")}, nil
}

func (h *holdProvider) second() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.reqs) < 2 {
		return ""
	}
	var b strings.Builder
	for _, m := range h.reqs[1].Messages {
		b.WriteString(m.Text())
		b.WriteByte('\n')
	}
	return b.String()
}

type blockProvider struct {
	once    sync.Once
	entered chan struct{}
}

func (b *blockProvider) Chat(ctx context.Context, _ provider.Request, _ func(string)) (provider.Message, error) {
	b.once.Do(func() { close(b.entered) })
	<-ctx.Done()
	return provider.Message{}, ctx.Err()
}

func testLink(t *testing.T) (*Link, string) {
	t.Helper()
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "persona", []string{link.NameCoding})
	if err != nil {
		t.Fatal(err)
	}
	return Open(bot.Dir(b.Root, b.Name()), b.InboxPath()), t.TempDir()
}

func mustStart(t *testing.T, l *Link, work string, steps, timeout int, goal string) string {
	t.Helper()
	args, _ := json.Marshal(map[string]any{
		"work_dir": work, "max_steps": steps, "timeout_sec": timeout, "goal": goal,
	})
	out, err := toolNamed(l, "code_start").Run(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func toolNamed(l *Link, name string) interface {
	Run(context.Context, json.RawMessage) (string, error)
} {
	for _, tool := range l.Tools() {
		if tool.Name() == name {
			return tool
		}
	}
	return nil
}

func waitCode(t *testing.T, l *Link, n int) []link.Event {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		box, err := inbox.Open(l.inbox)
		if err == nil && len(box.Pending()) >= n {
			return box.Pending()
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("timed out waiting for coding events")
	return nil
}

func waitIdle(t *testing.T, l *Link) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		l.mu.Lock()
		n := len(l.live)
		l.mu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("coding task still running")
}

func newCodeServer(t *testing.T, svc *Service) string {
	t.Helper()
	ui := httptest.NewServer(svc.Handler("tok"))
	t.Cleanup(ui.Close)
	return ui.URL
}

func TestRecoverKeepsQueuedAndPromotedBackendTasks(t *testing.T) {
	l, work := testLink(t)
	hold := newHold()
	l.UseProvider(hold)
	mustStart(t, l, work, 4, 30, "A")
	<-hold.entered
	b := strings.TrimPrefix(mustStart(t, l, work, 4, 30, "B"), "queued ")
	fresh := Open(filepath.Dir(l.rec.Dir), l.inbox)
	if err := fresh.Recover(); err != nil {
		t.Fatal(err)
	}
	m, err := l.rec.Get(b)
	if err != nil || m.Status != StatusQueued || m.PID == 0 {
		close(hold.release)
		t.Fatalf("live queued interrupted: %+v %v", m, err)
	}
	close(hold.release)
	waitCode(t, l, 2)
	m, err = l.rec.Get(b)
	if err != nil || m.Status != StatusDone || m.PID == 0 {
		t.Fatalf("promoted task lost ownership: %+v %v", m, err)
	}
}
