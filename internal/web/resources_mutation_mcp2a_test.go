package web

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/mcp"
	"xbot/internal/tools"
	"xbot/internal/vault"
)

type mutationHostFixture struct {
	s          *Server
	target     *bot.Bot
	constructs atomic.Int32
}

func newMutationHostFixture(t *testing.T) *mutationHostFixture {
	t.Helper()
	root := t.TempDir()
	keeper, err := bot.Create(root, "keeper", "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	target, err := bot.Create(root, "target", "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := bot.SetPrimary(root, keeper.Name()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	manager := mcp.NewManager(ctx, mcp.Options{})
	f := &mutationHostFixture{target: target, s: &Server{Root: root, ForkTurns: true}}
	f.s.NewResources = func(b *bot.Bot) (*kernel.BotResources, error) {
		r, err := kernel.NewBotResources(ctx, b, manager)
		if err == nil {
			r.Rename = f.s.RenameResources
			r.Delete = f.s.DeleteRuntimeBot
		}
		return r, err
	}
	f.s.NewKernel = func(b *bot.Bot, r *kernel.BotResources) (*kernel.Kernel, error) {
		f.constructs.Add(1)
		box, err := inbox.Open(b.InboxPath())
		if err != nil {
			return nil, err
		}
		return &kernel.Kernel{Bot: b, Resources: r, Inbox: box}, nil
	}
	t.Cleanup(func() { f.s.CloseOperations(); f.s.CloseResources(); cancel(); _ = manager.Close() })
	return f
}

func mutationAwait(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal(label)
	}
}

func TestMCP2aColdRenameRetainsEntryWithoutKernel(t *testing.T) {
	f := newMutationHostFixture(t)
	r, err := f.s.ResourcesFor(f.target)
	if err != nil {
		t.Fatal(err)
	}
	entry := f.s.kernels["target"]
	jobs, ref, gate := r.Jobs, r.MCP, entry.Gate
	if err := f.s.RenameRuntimeBot("target", "renamed"); err != nil {
		t.Fatal(err)
	}
	if f.constructs.Load() != 0 {
		t.Fatal("cold rename constructed a Kernel")
	}
	if f.s.kernels["renamed"] != entry || f.s.kernels["target"] != nil || entry.Gate != gate || entry.Resources.Jobs != jobs || entry.Resources.MCP != ref {
		t.Fatal("rename replaced shared entry/Gate/Jobs/ref")
	}
	current, handle, ok, err := r.TryCurrentHandle()
	if err != nil || !ok || current.Name() != "renamed" {
		t.Fatalf("current wake handle: %v %v %v", current, ok, err)
	}
	if err := handle.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bot.Dir(f.s.Root, "target")); !os.IsNotExist(err) {
		t.Fatalf("old path recreated: %v", err)
	}
}

func TestMCP2aDifferentRenameSourceInvalidatesCachedKernel(t *testing.T) {
	f := newMutationHostFixture(t)
	cached, gate, err := f.s.kernel(f.target)
	if err != nil {
		t.Fatal(err)
	}
	r := cached.Resources
	b, err := bot.Load(f.s.Root, "target")
	if err != nil {
		t.Fatal(err)
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	source := &kernel.Kernel{Bot: b, Inbox: box, Resources: r}
	if err := source.RenameRuntime("renamed"); err != nil {
		t.Fatal(err)
	}
	entry := f.s.kernels["renamed"]
	if entry.Kernel != nil || entry.Resources != r || entry.Gate != gate {
		t.Fatal("different cached source not invalidated in same entry")
	}
	if source.Bot.Name() != "renamed" || source.Inbox == box {
		t.Fatal("invoking source did not move Bot and Inbox")
	}
	if cached.Bot.Name() != "target" {
		t.Fatal("rename mutated another source's Bot asynchronously")
	}
}

func TestMCP2aDeleteProtectedFailureKeepsResourcesUsable(t *testing.T) {
	f := newMutationHostFixture(t)
	r, err := f.s.ResourcesFor(f.target)
	if err != nil {
		t.Fatal(err)
	}
	jobs, ref := r.Jobs, r.MCP
	if err := bot.SetPrimary(f.s.Root, "target"); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteRuntimeBot(context.Background(), "target"); !errors.Is(err, bot.ErrPrimary) {
		t.Fatalf("delete primary: %v", err)
	}
	if r.Jobs != jobs || r.MCP != ref || !r.AdmissionOpen() {
		t.Fatal("initial guard disturbed resources")
	}
	result := jobs.RunCall(context.Background(), tools.CallRequest{Name: "after-refusal", Run: func(context.Context) tools.CallResult { return tools.CallResult{Text: "usable"} }})
	if result.Err != nil || result.Text != "usable" {
		t.Fatalf("after refusal: %+v", result)
	}
}

func TestMCP2aDeleteJoinReleasesLocksAndResumesSameResources(t *testing.T) {
	f := newMutationHostFixture(t)
	r, err := f.s.ResourcesFor(f.target)
	if err != nil {
		t.Fatal(err)
	}
	entry, jobs, ref := f.s.kernels["target"], r.Jobs, r.MCP
	started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	receipt := jobs.RunCall(context.Background(), tools.CallRequest{Name: "uncooperative", Background: true, Run: func(ctx context.Context) tools.CallResult {
		close(started)
		<-ctx.Done()
		close(canceled)
		<-release
		return tools.CallResult{Text: "joined completion"}
	}})
	if receipt.Err != nil || receipt.Job == nil {
		t.Fatalf("start: %+v", receipt)
	}
	mutationAwait(t, started, "call did not start")
	deleted := make(chan error, 1)
	go func() { deleted <- f.s.DeleteRuntimeBot(context.Background(), "target") }()
	mutationAwait(t, canceled, "delete did not request cancellation")
	select {
	case err := <-deleted:
		t.Fatalf("delete did not join actual call: %v", err)
	default:
	}
	if _, err := bot.Load(f.s.Root, "target"); err != nil {
		t.Fatal("directory vanished during join", err)
	}
	// These operations must all finish while the owned call still ignores its
	// cancellation: host, route and primary locks are free during the join.
	checked := make(chan struct{})
	go func() {
		defer close(checked)
		if err := f.s.RenameRuntimeBot("target", "blocked"); err == nil {
			t.Error("rename admitted during delete")
		}
		if _, err := f.s.ResourcesFor(f.target); err == nil {
			t.Error("paused entry admitted Kernel construction")
		}
		if _, err := r.CurrentBot(); err != nil {
			t.Error("route unavailable during join", err)
		}
		if err := bot.SetPrimary(f.s.Root, "target"); err != nil {
			t.Error("primary unavailable during join", err)
		}
	}()
	mutationAwait(t, checked, "delete join retained a global/resource lock")
	once.Do(func() { close(release) })
	select {
	case err := <-deleted:
		if !errors.Is(err, bot.ErrPrimary) {
			t.Fatalf("late protection: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("delete did not finish")
	}
	if f.s.kernels["target"] != entry || r.Jobs != jobs || r.MCP != ref || !r.AdmissionOpen() {
		t.Fatal("late failure replaced or closed original resources")
	}
	box, err := inbox.Open(f.target.InboxPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(box.Pending()) != 1 {
		t.Fatalf("original OnDone lost during join: %+v", box.Pending())
	}
	if result := jobs.RunCall(context.Background(), tools.CallRequest{Name: "resumed", Run: func(context.Context) tools.CallResult { return tools.CallResult{Text: "same table"} }}); result.Err != nil {
		t.Fatal(result.Err)
	}
}

func TestMCP2aDeletePhysicalFailureResumesAndCleanupFailureCloses(t *testing.T) {
	t.Run("before removal", func(t *testing.T) {
		f := newMutationHostFixture(t)
		r, err := f.s.ResourcesFor(f.target)
		if err != nil {
			t.Fatal(err)
		}
		// A real filesystem failure opening the required global migration vault.
		if err := os.WriteFile(filepath.Join(f.s.Root, "vault"), []byte("blocked"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := f.s.DeleteRuntimeBot(context.Background(), "target"); err == nil {
			t.Fatal("expected migration filesystem error")
		}
		if !r.AdmissionOpen() || f.s.kernels["target"].Resources != r {
			t.Fatal("pre-removal failure did not resume same resources")
		}
		if _, err := bot.Load(f.s.Root, "target"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("after removal", func(t *testing.T) {
		f := newMutationHostFixture(t)
		r, err := f.s.ResourcesFor(f.target)
		if err != nil {
			t.Fatal(err)
		}
		// No old ciphertext exists, so migration ignores this legacy key. Its
		// nonempty directory fails only the original post-RemoveAll cleanup.
		key := filepath.Join(filepath.Dir(vault.GlobalKeyPath(f.s.Root)), "target.key")
		if err := os.MkdirAll(key, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(key, "block"), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := f.s.DeleteRuntimeBot(context.Background(), "target"); err == nil {
			t.Fatal("expected post-removal key cleanup error")
		}
		if f.s.kernels["target"] != nil || r.AdmissionOpen() {
			t.Fatal("cleanup error resurrected removed entry")
		}
		if _, err := os.Stat(bot.Dir(f.s.Root, "target")); !os.IsNotExist(err) {
			t.Fatalf("physical directory remains: %v", err)
		}
		if err := r.ResumeAdmission(); err == nil {
			t.Fatal("removed resource reopened")
		}
		if result := r.Jobs.RunCall(context.Background(), tools.CallRequest{Name: "too-late", Run: func(context.Context) tools.CallResult { t.Error("removed call ran"); return tools.CallResult{} }}); result.Err == nil {
			t.Fatal("removed table admitted call")
		}
		if _, err := os.Stat(bot.Dir(f.s.Root, "target")); !os.IsNotExist(err) {
			t.Fatalf("late admission recreated directory: %v", err)
		}
	})
}
