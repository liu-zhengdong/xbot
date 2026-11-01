package exec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/provider"
	"xbot/internal/work"
)

type moveBotFixture struct {
	root, name string
	route      sync.RWMutex
}

func newMoveBotFixture(t *testing.T) *moveBotFixture {
	t.Helper()
	f := &moveBotFixture{root: t.TempDir(), name: "before"}
	for _, dir := range []string{f.agents(), work.BotDir(f.root, f.name)} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return f
}
func (f *moveBotFixture) agents() string { return filepath.Join(bot.Dir(f.root, f.name), "agents") }
func (f *moveBotFixture) local() *Local {
	l := NewLocal(Limits{MaxConcurrent: 1, MaxSteps: 4, ProgressEvery: -1})
	l.AgentsDir = f.agents()
	l.WithCurrentIO = func(fn func(string, string, string) error) error {
		f.route.RLock()
		defer f.route.RUnlock()
		return fn(f.name, f.agents(), work.BotDir(f.root, f.name))
	}
	l.Prov = moveAnswer{}
	l.Restore = func(_ context.Context, d Descriptor) (StartRequest, error) {
		return StartRequest{ID: d.ID, Bot: d.Bot, Dir: filepath.Join(l.AgentsDir, d.ID), Workspace: work.BotDir(f.root, d.Bot), Resource: "dir:" + d.CWD, Mode: d.Mode, Descriptor: &d}, nil
	}
	return l
}
func (f *moveBotFixture) move(next string, after error) (bool, error) {
	oldAgents := f.agents()
	newAgents := filepath.Join(bot.Dir(f.root, next), "agents")
	return MoveBot(oldAgents, newAgents, func() (bool, error) {
		f.route.Lock()
		defer f.route.Unlock()
		if err := os.Rename(bot.Dir(f.root, f.name), bot.Dir(f.root, next)); err != nil {
			return false, err
		}
		err := os.Rename(work.BotDir(f.root, f.name), work.BotDir(f.root, next))
		f.name = next
		return true, errors.Join(err, after)
	})
}
func (f *moveBotFixture) request(id string, continuable bool) StartRequest {
	req := StartRequest{ID: id, Bot: f.name, Task: "task", Mode: ModeBrief, Dir: filepath.Join(f.agents(), id), Workspace: work.BotDir(f.root, f.name), Resource: "dir:" + work.BotDir(f.root, f.name)}
	if continuable {
		req.Descriptor = &Descriptor{Version: 1, ID: id, Bot: f.name, Mode: ModeBrief, CWD: filepath.Join(req.Workspace, "project"), Provider: "selected", Model: "original", MaxSteps: 4}
	}
	return req
}

type moveAnswer struct{}

func (moveAnswer) Chat(context.Context, provider.Request, func(string)) (provider.Message, error) {
	return answer("done"), nil
}

type moveGate struct {
	entered, release chan struct{}
	once             sync.Once
}

func (g *moveGate) Chat(ctx context.Context, _ provider.Request, _ func(string)) (provider.Message, error) {
	g.once.Do(func() { close(g.entered) })
	select {
	case <-g.release:
		return answer("done"), nil
	case <-ctx.Done():
		return provider.Message{}, ctx.Err()
	}
}
func moveTerminal(t *testing.T, l *Local) Event {
	t.Helper()
	for {
		select {
		case e := <-l.Events():
			if e.Kind == EvDone || e.Kind == EvFailed {
				return e
			}
		case <-time.After(3 * time.Second):
			t.Fatal("terminal event timed out")
			return Event{}
		}
	}
}

func TestMoveBotKeepsLegacyAndContinuableOwners(t *testing.T) {
	f := newMoveBotFixture(t)
	legacy, child := f.local(), f.local()
	a := &moveGate{entered: make(chan struct{}), release: make(chan struct{})}
	b := &moveGate{entered: make(chan struct{}), release: make(chan struct{})}
	legacy.Prov = a
	child.Prov = b
	oldAgents := f.agents()
	if _, err := legacy.Start(context.Background(), f.request("sa_legacy0001", false)); err != nil {
		t.Fatal(err)
	}
	if _, err := child.Start(context.Background(), f.request("sa_child00001", true)); err != nil {
		t.Fatal(err)
	}
	<-a.entered
	<-b.entered
	if _, ok := ActiveTask(filepath.Join(oldAgents, "sa_legacy0001")); !ok {
		t.Fatal("legacy accepted owner missing")
	}
	partial := errors.New("after move cleanup")
	if moved, err := f.move("after", partial); !moved || !errors.Is(err, partial) {
		t.Fatalf("move=%v err=%v", moved, err)
	}
	for _, id := range []string{"sa_legacy0001", "sa_child00001"} {
		if _, ok := ActiveTask(filepath.Join(oldAgents, id)); ok {
			t.Fatal("old owner key survived")
		}
		if _, ok := ActiveTask(filepath.Join(f.agents(), id)); !ok {
			t.Fatalf("new owner missing: %s", id)
		}
	}
	// Another empty Local routes legacy cancellation to its actual owner.
	if err := f.local().Control(context.Background(), "sa_legacy0001", Control{Kind: KindCancel}); err != nil {
		t.Fatal(err)
	}
	close(a.release)
	close(b.release)
	moveTerminal(t, legacy)
	moveTerminal(t, child)
	for _, id := range []string{"sa_legacy0001", "sa_child00001"} {
		if _, err := ReadMeta(filepath.Join(f.agents(), id)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Dir(oldAgents)); !os.IsNotExist(err) {
		t.Fatalf("old bot directory recreated: %v", err)
	}
	d, err := ReadDescriptor(filepath.Join(f.agents(), "sa_child00001"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Bot != "after" || !strings.HasPrefix(d.CWD, work.BotDir(f.root, "after")) || d.Provider != "selected" || d.Model != "original" {
		t.Fatalf("descriptor=%+v", d)
	}
}

func TestMoveBotDoesNotHoldStorageAcrossCurrentCWD(t *testing.T) {
	f := newMoveBotFixture(t)
	l := f.local()
	entered, release := make(chan struct{}), make(chan struct{})
	oldWork := work.BotDir(f.root, "before")
	l.CurrentCWD = func(StartRequest) string { close(entered); <-release; return filepath.Join(oldWork, "project") }
	if _, err := l.Start(context.Background(), f.request("sa_cwd000001", true)); err != nil {
		t.Fatal(err)
	}
	<-entered
	moved := make(chan error, 1)
	go func() { _, err := f.move("after", nil); moved <- err }()
	select {
	case err := <-moved:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("rename waited on CurrentCWD callback")
	}
	close(release)
	moveTerminal(t, l)
	d, err := ReadDescriptor(filepath.Join(f.agents(), "sa_cwd000001"))
	if err != nil {
		t.Fatal(err)
	}
	if d.CWD != filepath.Join(work.BotDir(f.root, "after"), "project") {
		t.Fatalf("stale cwd: %s", d.CWD)
	}
}

func TestMoveBotColdRestoreRebasesAfterUnlockedAssembly(t *testing.T) {
	f := newMoveBotFixture(t)
	l := f.local()
	id := "sa_cold00001"
	if _, err := l.Start(context.Background(), f.request(id, true)); err != nil {
		t.Fatal(err)
	}
	moveTerminal(t, l)
	// Wait for actual owner removal, not only the terminal send.
	owners.Lock()
	l.mu.Lock()
	l.mu.Unlock()
	owners.Unlock()
	cold := f.local()
	entered, release := make(chan struct{}), make(chan struct{})
	original := cold.Restore
	cold.Restore = func(ctx context.Context, d Descriptor) (StartRequest, error) {
		req, err := original(ctx, d)
		close(entered)
		<-release
		return req, err
	}
	result := make(chan error, 1)
	go func() { _, err := cold.Message(context.Background(), id, Control{Text: "continue"}); result <- err }()
	<-entered
	if moved, err := f.move("after", nil); !moved || err != nil {
		t.Fatalf("move=%v err=%v", moved, err)
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	e := moveTerminal(t, cold)
	if e.TurnSeq <= 1 {
		t.Fatalf("turn identity was reset: %+v", e)
	}
	d, err := ReadDescriptor(filepath.Join(f.agents(), id))
	if err != nil {
		t.Fatal(err)
	}
	if d.Bot != "after" || d.Provider != "selected" || d.Model != "original" {
		t.Fatalf("restored policy=%+v", d)
	}
	m, err := ReadMeta(filepath.Join(f.agents(), id))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(m.ResultPath, f.agents()) {
		t.Fatalf("stale result path: %s", m.ResultPath)
	}
	if _, err := os.Stat(filepath.Join(bot.Dir(f.root, "before"), "agents", id)); !os.IsNotExist(err) {
		t.Fatalf("old directory recreated: %v", err)
	}
}
