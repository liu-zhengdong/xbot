package code

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/provider"
	"xbot/internal/work"
)

type currentCodeFixture struct {
	route sync.RWMutex
	b     *bot.Bot
	box   *inbox.Inbox
}

func newCurrentCodeFixture(t *testing.T) *currentCodeFixture {
	t.Helper()
	b, err := bot.Create(t.TempDir(), "before", "persona", []string{inbox.NameCoding})
	if err != nil {
		t.Fatal(err)
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	return &currentCodeFixture{b: b, box: box}
}
func (f *currentCodeFixture) io(fn func(*bot.Bot, *inbox.Inbox) error) error {
	f.route.RLock()
	defer f.route.RUnlock()
	return fn(f.b, f.box)
}
func (f *currentCodeFixture) open() *Link {
	l := Open(bot.Dir(f.b.Root, f.b.Name()), f.b.InboxPath())
	l.UseCurrentIO(f.io)
	return l
}
func (f *currentCodeFixture) rename() error {
	f.route.Lock()
	defer f.route.Unlock()
	if err := bot.Rename(f.b.Root, f.b.Name(), "after"); err != nil {
		return err
	}
	b, err := bot.Load(f.b.Root, "after")
	if err != nil {
		return err
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		return err
	}
	f.b, f.box = b, box
	return nil
}
func (f *currentCodeFixture) move(source *Link, before func()) error {
	return source.Move(bot.Dir(f.b.Root, "after"), filepath.Join(bot.Dir(f.b.Root, "after"), "inbox.jsonl"), nil, func() error {
		if before != nil {
			before()
		}
		return f.rename()
	})
}

type currentCodeGate struct {
	entered, release chan struct{}
	once             sync.Once
}

func (g *currentCodeGate) Chat(ctx context.Context, _ provider.Request, _ func(string)) (provider.Message, error) {
	g.once.Do(func() { close(g.entered) })
	select {
	case <-g.release:
		return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("finished after rename")}, nil
	case <-ctx.Done():
		return provider.Message{}, ctx.Err()
	}
}

func TestCurrentCodeOtherLinkFinishesAndPostsAfterMove(t *testing.T) {
	f := newCurrentCodeFixture(t)
	source, old := f.open(), f.open()
	oldDir := bot.Dir(f.b.Root, f.b.Name())
	gate := &currentCodeGate{entered: make(chan struct{}), release: make(chan struct{})}
	old.UseProvider(gate)
	finished := make(chan Task, 1)
	old.rec.OnTerminal = func(task Task) {
		if !old.rec.mu.TryLock() {
			t.Error("terminal callback held Record.mu")
		} else {
			old.rec.mu.Unlock()
		}
		if !f.route.TryLock() {
			t.Error("terminal callback held route")
		} else {
			f.route.Unlock()
		}
		old.post(task)
		finished <- task
	}
	task, err := old.rec.Start(Task{WorkDir: t.TempDir(), Goal: "complete", MaxSteps: 2, TimeoutSec: 10})
	if err != nil {
		t.Fatal(err)
	}
	old.launch(task)
	<-gate.entered
	if err := f.move(source, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := old.rec.Control(task.ID, KindSteer, "new note"); err != nil {
		t.Fatal(err)
	}
	old.stream(task.ID, StatusRunning, "card after rename")
	close(gate.release)
	select {
	case got := <-finished:
		if got.ID != task.ID || got.Status != StatusDone {
			t.Fatalf("terminal=%+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("completion timed out")
	}
	got, err := old.rec.Get(task.ID)
	if err != nil || got.Status != StatusDone || len(got.Notes) != 1 {
		t.Fatalf("record=%+v %v", got, err)
	}
	if len(f.box.Pending()) != 1 {
		t.Fatalf("pending=%+v", f.box.Pending())
	}
	entries, err := f.b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for _, e := range entries {
		text.WriteString(e.Message.Text())
	}
	if !strings.Contains(text.String(), "card after rename") || !strings.Contains(text.String(), `status="done"`) {
		t.Fatalf("cards=%s", text.String())
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("old bot directory recreated: %v", err)
	}
}

func TestCurrentCodeFlockWaitReleasesRouteForMove(t *testing.T) {
	f := newCurrentCodeFixture(t)
	source, other := f.open(), f.open()
	task, err := source.rec.Start(Task{WorkDir: t.TempDir(), Goal: "record", MaxSteps: 2, TimeoutSec: 10})
	if err != nil {
		t.Fatal(err)
	}
	atMove, releaseMove := make(chan struct{}), make(chan struct{})
	moved := make(chan error, 1)
	go func() { moved <- f.move(source, func() { close(atMove); <-releaseMove }) }()
	<-atMove
	returnedIO := make(chan struct{}, 1)
	other.UseCurrentIO(func(fn func(*bot.Bot, *inbox.Inbox) error) error {
		err := f.io(fn)
		select {
		case returnedIO <- struct{}{}:
		default:
		}
		return err
	})
	read := make(chan error, 1)
	go func() { _, err := other.rec.Get(task.ID); read <- err }()
	select {
	case <-returnedIO:
	case <-time.After(time.Second):
		close(releaseMove)
		t.Fatal("flock waiter retained route")
	}
	close(releaseMove)
	select {
	case err := <-moved:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("rename blocked by flock waiter route")
	}
	select {
	case err := <-read:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("record did not resolve moved path")
	}
}

// Both calls are selected before rename; their actual file I/O and shell Start
// must still resolve after the model response arrives on the renamed Bot.
type currentCodeCwdGate struct {
	entered, release chan struct{}
	n                int
	path             string
}

func (g *currentCodeCwdGate) Chat(ctx context.Context, _ provider.Request, _ func(string)) (provider.Message, error) {
	g.n++
	if g.n > 1 {
		return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("done")}, nil
	}
	close(g.entered)
	select {
	case <-g.release:
	case <-ctx.Done():
		return provider.Message{}, ctx.Err()
	}
	args, _ := json.Marshal(map[string]string{"path": g.path, "content": "new workspace"})
	return provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{
		{ID: "write", Type: "function", Function: provider.FunctionCall{Name: "write_file", Arguments: string(args)}},
		{ID: "shell", Type: "function", Function: provider.FunctionCall{Name: "sh", Arguments: `{"description":"record cwd","command":"pwd > shell-cwd.txt"}`}},
	})}, nil
}

func TestCurrentCodeModelToolsFollowWorkspaceAcrossMove(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is unavailable")
	}
	f := newCurrentCodeFixture(t)
	source, old := f.open(), f.open()
	oldWork := work.BotDir(f.b.Root, f.b.Name())
	gate := &currentCodeCwdGate{entered: make(chan struct{}), release: make(chan struct{}), path: filepath.Join(oldWork, "project", "created.txt")}
	old.UseProvider(gate)
	finished := make(chan Task, 1)
	old.rec.OnTerminal = func(task Task) { old.post(task); finished <- task }
	task, err := old.rec.Start(Task{WorkDir: filepath.Join(oldWork, "project"), Goal: "write two files", MaxSteps: 3, TimeoutSec: 10})
	if err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	queued, err := source.rec.Start(Task{WorkDir: external, Repo: filepath.Join(external, "repo"), Goal: "external", MaxSteps: 1, TimeoutSec: 10})
	if err != nil {
		t.Fatal(err)
	}
	old.launch(task)
	<-gate.entered
	if err := f.move(source, nil); err != nil {
		t.Fatal(err)
	}
	ext, err := source.rec.Get(queued.ID)
	if err != nil || ext.WorkDir != external || ext.Repo != filepath.Join(external, "repo") {
		t.Fatalf("external paths changed: %+v %v", ext, err)
	}
	if _, err := source.rec.Control(queued.ID, KindCancel, ""); err != nil {
		t.Fatal(err)
	}
	close(gate.release)
	select {
	case got := <-finished:
		if got.Status != StatusDone {
			t.Fatalf("terminal=%+v", got)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("model did not finish")
	}
	project := filepath.Join(work.BotDir(f.b.Root, "after"), "project")
	raw, err := os.ReadFile(filepath.Join(project, "created.txt"))
	if err != nil || string(raw) != "new workspace" {
		t.Fatalf("file=%q %v", raw, err)
	}
	cwd, err := os.ReadFile(filepath.Join(project, "shell-cwd.txt"))
	if err != nil || strings.TrimSpace(string(cwd)) != canonicalCodeTestPath(t, project) {
		t.Fatalf("shell cwd=%q %v", cwd, err)
	}
	if _, err := os.Stat(oldWork); !os.IsNotExist(err) {
		t.Fatalf("old workspace recreated: %v", err)
	}
}

func TestCurrentCodeCommandStartResolvesAfterEnvironmentGate(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is unavailable")
	}
	f := newCurrentCodeFixture(t)
	source, old := f.open(), f.open()
	script := filepath.Join(t.TempDir(), "fake-agent")
	if err := os.WriteFile(script, []byte("#!/bin/sh\npwd > process-cwd.txt\nprintf '%s' \"$XBOT_CODE_REPO\" > repo-env.txt\necho done\n"), 0700); err != nil {
		t.Fatal(err)
	}
	old.UseCommand(script)
	entered, release := make(chan struct{}), make(chan struct{})
	old.UseEnvironment(func(env []string) []string { close(entered); <-release; return env }, nil)
	finished := make(chan Task, 1)
	old.rec.OnTerminal = func(task Task) { old.post(task); finished <- task }
	oldWork := work.BotDir(f.b.Root, "before")
	for _, dir := range []string{filepath.Join(oldWork, "project", ".git"), filepath.Join(oldWork, "repo")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	task, err := old.rec.Start(Task{WorkDir: filepath.Join(oldWork, "project"), Repo: filepath.Join(oldWork, "repo"), Goal: "run", MaxSteps: 2, TimeoutSec: 10})
	if err != nil {
		t.Fatal(err)
	}
	old.launch(task)
	<-entered
	if err := f.move(source, nil); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case got := <-finished:
		if got.Status != StatusDone {
			t.Fatalf("terminal=%+v", got)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("command did not finish")
	}
	project := filepath.Join(work.BotDir(f.b.Root, "after"), "project")
	cwd, err := os.ReadFile(filepath.Join(project, "process-cwd.txt"))
	if err != nil || strings.TrimSpace(string(cwd)) != canonicalCodeTestPath(t, project) {
		t.Fatalf("process cwd=%q %v", cwd, err)
	}
	repo, err := os.ReadFile(filepath.Join(project, "repo-env.txt"))
	if err != nil || string(repo) != filepath.Join(work.BotDir(f.b.Root, "after"), "repo") {
		t.Fatalf("process repo=%q %v", repo, err)
	}
	if _, err := os.Stat(oldWork); !os.IsNotExist(err) {
		t.Fatalf("old workspace recreated: %v", err)
	}
}

func canonicalCodeTestPath(t *testing.T, path string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return real
}
