package e2e

import (
	"bytes"
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/exec"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
	"xbot/internal/memory"
	"xbot/internal/mockopenai"
	"xbot/internal/notify"
	"xbot/internal/proc"
	"xbot/internal/provider"
	"xbot/internal/serve"
	"xbot/internal/tools"
)

func TestRoutineHandledWhileChat(t *testing.T) {
	root := t.TempDir()
	srv := httptest.NewServer(mockopenai.NewMux(t.TempDir()))
	defer srv.Close()
	client := &provider.Client{BaseURL: srv.URL + "/v1", APIKey: "test", Model: "mock", HTTPClient: srv.Client()}
	b := mustBot(t, root, "chatting")
	now := time.Date(2026, 10, 15, 8, 7, 0, 0, time.UTC)
	if err := (&link.Timer{Dir: b.RoutineDir(), Sender: "u1"}).Save(link.Routine{
		ID: "mail", Name: "Mail", Prompt: "NOTIFY the owner", Cron: "7 * * * *", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	k := m3Kernel(t, b, client, []link.Link{link.Terminal{}, &link.Timer{Dir: b.RoutineDir(), Sender: "u1"}}, nil)

	var buf safeBuf
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lines := make(chan proc.Line)
	errCh := make(chan error, 1)
	go func() {
		errCh <- proc.Run(ctx, proc.Config{
			K:           k,
			Poll:        20 * time.Millisecond,
			SkipDeliver: true,
			Lines:       lines,
			Accept:      func(string) (link.Event, error) { return link.Event{}, nil },
			After: func(link.Event, error) {
				notes, err := notify.Open(b.OutboxPath()).Unread()
				if err != nil {
					return
				}
				var ids []string
				for _, n := range notes {
					fmt.Fprintf(&buf, "notification: %s\n", n.Text)
					ids = append(ids, n.ID)
				}
				if len(ids) > 0 {
					_ = notify.Open(b.OutboxPath()).MarkRead(ids)
				}
			},
		})
	}()

	time.Sleep(40 * time.Millisecond)
	if err := serve.Pass(ctx, root, now, func(bb *bot.Bot) (*kernel.Kernel, error) {
		kk := routineKernel(bb, client)
		box, err := inbox.Open(bb.InboxPath())
		if err != nil {
			return nil, err
		}
		kk.Inbox = box
		return kk, nil
	}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 2*time.Second, func() bool {
		notes, err := notify.Open(b.OutboxPath()).Unread()
		body := dump(mustEntries(t, b))
		got := (err == nil && len(notes) == 1 && notes[0].Text == "mail is waiting") || strings.Contains(buf.String(), "mail is waiting")
		return got && strings.Count(body, "noted") == 1
	})
	if n := strings.Count(dump(mustEntries(t, b)), "noted"); n != 1 {
		t.Fatalf("routine handled %d times\n%s", n, dump(mustEntries(t, b)))
	}
	if err := serve.Pass(ctx, root, now, func(bb *bot.Bot) (*kernel.Kernel, error) {
		kk := routineKernel(bb, client)
		box, err := inbox.Open(bb.InboxPath())
		if err != nil {
			return nil, err
		}
		kk.Inbox = box
		return kk, nil
	}); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(dump(mustEntries(t, b)), "noted"); n != 1 {
		t.Fatalf("second pass handled again\n%s", dump(mustEntries(t, b)))
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("processor did not stop")
	}
}

func TestSpawnWhileChatKeepsAnswering(t *testing.T) {
	script := mockopenai.NewScript()
	srv := httptest.NewServer(mockopenai.NewScriptMux(t.TempDir(), script))
	defer srv.Close()
	client := &provider.Client{BaseURL: srv.URL + "/v1", APIKey: "test", Model: "mock", HTTPClient: srv.Client()}
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "You are Ada.", []string{link.NameTerminal, link.NameAgents})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	loc := newLocal(client)
	k := m3Kernel(t, b, client, []link.Link{link.Terminal{}, link.Agents{}}, loc)
	var buf safeBuf
	k.OnDelta = func(s string) { buf.WriteString(s) }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lines := make(chan proc.Line, 4)
	go func() {
		_ = proc.Run(ctx, proc.Config{
			K:      k,
			Poll:   20 * time.Millisecond,
			Lines:  lines,
			Accept: terminalAccept(k, "u1"),
		})
	}()
	lines <- proc.Line{Text: "SPAWN_LONG\n"}
	waitText(t, &buf, "started")
	if err := script.WaitEntered(context.Background(), "long"); err != nil {
		t.Fatal(err)
	}
	lines <- proc.Line{Text: "ping\n"}
	waitText(t, &buf, "pong")
	if strings.Contains(buf.String(), "saw subagent done") {
		t.Fatal("done event was handled before the chat reply")
	}
	script.Release("long")
	waitText(t, &buf, "saw subagent done")
	out := buf.String()
	if strings.Index(out, "pong") > strings.Index(out, "saw subagent done") {
		t.Fatalf("order:\n%s", out)
	}
}

func TestSteerInterruptCancel(t *testing.T) {
	testControls(t, false)
}

func TestSteerInterruptCancelViaRunner(t *testing.T) {
	testControls(t, true)
}

func testControls(t *testing.T, viaRunner bool) {
	t.Helper()
	script := mockopenai.NewScript()
	srv := httptest.NewServer(mockopenai.NewScriptMux(t.TempDir(), script))
	defer srv.Close()
	client := &provider.Client{BaseURL: srv.URL + "/v1", APIKey: "test", Model: "mock", HTTPClient: srv.Client()}
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "You are Ada.", []string{link.NameTerminal, link.NameAgents})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	ex, done := executorFor(t, client, viaRunner)
	defer done()
	k := m3Kernel(t, b, client, []link.Link{link.Terminal{}, link.Agents{}}, ex)

	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "SPAWN_STEER", time.Now())); err != nil {
		t.Fatal(err)
	}
	if err := script.WaitEntered(context.Background(), "steer"); err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	id := waitAgent(t, b, known)
	known[id] = true
	if err := ex.Control(context.Background(), id, exec.Control{Kind: exec.KindSteer, Text: "apply STEER_MARK now"}); err != nil {
		t.Fatal(err)
	}
	script.Release("steer")
	waitSub(t, b, id, "kept prior work")
	text := subText(t, b, id)
	if !strings.Contains(text, "PRIOR_WORK") || !strings.Contains(text, "STEER_MARK") || !strings.Contains(text, "kept prior work") {
		t.Fatalf("steer transcript:\n%s", text)
	}

	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "SPAWN_INTERRUPT", time.Now())); err != nil {
		t.Fatal(err)
	}
	if err := script.WaitEntered(context.Background(), "intr"); err != nil {
		t.Fatal(err)
	}
	id = waitAgent(t, b, known)
	known[id] = true
	if err := ex.Control(context.Background(), id, exec.Control{Kind: exec.KindInterrupt, Text: "INTERRUPT_NOW"}); err != nil {
		t.Fatal(err)
	}
	waitSub(t, b, id, "restarted after interrupt")
	text = subText(t, b, id)
	if !strings.Contains(text, "PRIOR_STEP") || !strings.Contains(text, "INTERRUPT_NOW") || !strings.Contains(text, "restarted after interrupt") {
		t.Fatalf("interrupt transcript:\n%s", text)
	}

	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "SPAWN_CANCEL", time.Now())); err != nil {
		t.Fatal(err)
	}
	if err := script.WaitEntered(context.Background(), "cancel"); err != nil {
		t.Fatal(err)
	}
	id = waitAgent(t, b, known)
	known[id] = true
	if err := ex.Control(context.Background(), id, exec.Control{Kind: exec.KindCancel}); err != nil {
		t.Fatal(err)
	}
	waitSub(t, b, id, "cancelled")
	meta, _, err := exec.ReadStatus(b.AgentDir(id))
	if err != nil {
		t.Fatal(err)
	}
	if meta.Status != "cancelled" {
		t.Fatalf("status %s", meta.Status)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		err = ex.Control(context.Background(), id, exec.Control{Kind: exec.KindCancel})
		if err != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("cancel did not stop the subagent: %v", err)
	}
}

func TestForkSeesContextBriefDoesNot(t *testing.T) {
	script := mockopenai.NewScript()
	srv := httptest.NewServer(mockopenai.NewScriptMux(t.TempDir(), script))
	defer srv.Close()
	client := &provider.Client{BaseURL: srv.URL + "/v1", APIKey: "test", Model: "mock", HTTPClient: srv.Client()}
	root := t.TempDir()
	b, err := bot.Create(root, "ada", "You are Ada.", []string{link.NameTerminal, link.NameAgents})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.SetOwners([]string{"u1"}); err != nil {
		t.Fatal(err)
	}
	loc := newLocal(client)
	k := m3Kernel(t, b, client, []link.Link{link.Terminal{}, link.Agents{}}, loc)
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "MARKER_7f3a", time.Now())); err != nil {
		t.Fatal(err)
	}
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "SPAWN_FORK", time.Now())); err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	forkID := waitAgent(t, b, known)
	known[forkID] = true
	waitSub(t, b, forkID, "fork done")
	if err := k.Handle(context.Background(), link.TerminalEvent("u1", "SPAWN_BRIEF", time.Now())); err != nil {
		t.Fatal(err)
	}
	briefID := waitAgent(t, b, known)
	waitSub(t, b, briefID, "brief done")

	var forkBody, briefBody string
	for _, body := range script.Bodies() {
		if strings.Contains(body, `Task:\nFORK_TASK`) {
			forkBody = body
		}
		if strings.Contains(body, exec.SubPersona) && strings.Contains(body, "BRIEF_TASK") {
			briefBody = body
		}
	}
	if forkBody == "" || !strings.Contains(forkBody, "MARKER_7f3a") {
		t.Fatal("fork subagent did not receive main context")
	}
	if briefBody == "" || strings.Contains(briefBody, "MARKER_7f3a") {
		t.Fatal("brief subagent saw main context")
	}
}

func m3Kernel(t *testing.T, b *bot.Bot, client *provider.Client, links []link.Link, ex exec.Executor) *kernel.Kernel {
	t.Helper()
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	return &kernel.Kernel{
		Bot: b, Provider: client, Links: links, Inbox: box, Exec: ex,
		Memory: memory.Open(b.Root), MaxSteps: 6, Budget: compact.DefaultBudget(),
		Approve:         func(context.Context, string, string) (bool, error) { return true, nil },
		AllowBackground: true,
		Desktop:         func(context.Context, string, string) error { return nil },
	}
}

func newLocal(client *provider.Client) *exec.Local {
	loc := exec.NewLocal(exec.Limits{Timeout: 30 * time.Second, ProgressEvery: -1, MaxSteps: 8})
	loc.Prov = client
	loc.Approve = func(context.Context, string, string) (bool, error) { return true, nil }
	loc.ToolSet = func(string) []tools.Tool {
		return []tools.Tool{tools.NewShell(), &tools.ReadFile{}, &tools.WriteFile{}, &tools.EditFile{}}
	}
	return loc
}

func executorFor(t *testing.T, client *provider.Client, viaRunner bool) (exec.Executor, func()) {
	t.Helper()
	if !viaRunner {
		return newLocal(client), func() {}
	}
	loc := exec.NewLocal(exec.Limits{Timeout: 30 * time.Second, ProgressEvery: -1, MaxSteps: 8})
	loc.Approve = func(context.Context, string, string) (bool, error) { return true, nil }
	loc.ToolSet = func(string) []tools.Tool {
		return []tools.Tool{tools.NewShell(), &tools.ReadFile{}, &tools.WriteFile{}, &tools.EditFile{}}
	}
	ts := httptest.NewServer((&exec.Server{Local: loc, Token: "tok"}).Handler())
	remote := exec.Dial(ts.URL, "tok")
	return remote, func() {
		remote.Close()
		ts.Close()
	}
}

func terminalAccept(k *kernel.Kernel, who string) func(string) (link.Event, error) {
	return func(text string) (link.Event, error) {
		text = strings.TrimSpace(text)
		if text == "" {
			return link.Event{}, nil
		}
		ev := link.TerminalEvent(who, text, time.Now())
		if err := k.Inbox.Post(ev); err != nil {
			return link.Event{}, err
		}
		return ev, nil
	}
}

type safeBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuf) WriteString(text string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.WriteString(text)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func waitUntil(t *testing.T, d time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("timed out")
}

func waitText(t *testing.T, buf *safeBuf, needle string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), needle) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q\n%s", needle, buf.String())
}

func waitSub(t *testing.T, b *bot.Bot, id, needle string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		last = subText(t, b, id)
		meta, _, err := exec.ReadStatus(b.AgentDir(id))
		if err == nil && (strings.Contains(last, needle) || strings.Contains(meta.Summary, needle) || meta.Status == "cancelled" && needle == "cancelled") {
			if strings.Contains(last, needle) || strings.Contains(meta.Summary, needle) || needle == "cancelled" {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("subagent %s missing %q\n%s", id, needle, last)
}

func subText(t *testing.T, b *bot.Bot, id string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(b.AgentDir(id), "transcript.jsonl"))
	if err != nil {
		return ""
	}
	return string(raw)
}

func waitAgent(t *testing.T, b *bot.Bot, known map[string]bool) string {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	dir := filepath.Dir(b.AgentDir("placeholder"))
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), "sa_") && !known[e.Name()] {
					return e.Name()
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for a subagent directory")
	return ""
}

func mustEntries(t *testing.T, b *bot.Bot) []bot.Entry {
	t.Helper()
	entries, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func dump(entries []bot.Entry) string {
	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "%s %s\n", e.Message.Role, e.Message.Content)
	}
	return b.String()
}
