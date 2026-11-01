package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/code"
	"xbot/internal/link"
	"xbot/internal/mcp"
	"xbot/internal/tools"
)

func TestMCP2aResourceRenameKeepsSessionAndRoutes(t *testing.T) {
	root := t.TempDir()
	b, err := bot.Create(root, "before", "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var initializes atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		var result any = map[string]any{}
		switch request.Method {
		case "initialize":
			initializes.Add(1)
			w.Header().Set("Mcp-Session-Id", "same-session")
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
			return
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "slow", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "finished once"}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}))
	defer remote.Close()
	config := fmt.Sprintf(`{"servers":[{"name":"fixture","url":%q}]}`, remote.URL)
	if err := os.WriteFile(b.MCPPath(), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := mcp.NewManager(ctx, mcp.Options{})
	defer manager.Close()
	resources, err := NewBotResources(ctx, b, manager)
	if err != nil {
		t.Fatal(err)
	}
	defer resources.Close()
	defer once.Do(func() { close(release) })
	originalJobs, originalRef := resources.Jobs, resources.MCP
	coding := code.Open(bot.Dir(root, b.Name()), b.InboxPath())
	k := &Kernel{Bot: b, Inbox: resources.box, Resources: resources, Links: []link.Link{coding}}
	stale := &Kernel{Bot: b, Inbox: resources.box, Resources: resources}
	finished := make(chan struct{})
	receipt := resources.Jobs.RunCall(ctx, tools.CallRequest{Name: "fixture", Background: true,
		Prepare: func(ctx context.Context, _ string) (context.Context, func()) { return ctx, func() { close(finished) } },
		Run: func(ctx context.Context) tools.CallResult {
			result, err := resources.MCP.CallTool(ctx, "fixture", "slow", json.RawMessage(`{}`))
			return tools.CallResult{Text: string(result), Err: err}
		},
	})
	if receipt.Err != nil || receipt.Job == nil {
		t.Fatalf("start: %+v", receipt)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("call never started")
	}
	if err := resources.postMCP("fixture", "before move"); err != nil {
		t.Fatal(err)
	}
	if err := k.RenameRuntime("after"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if err := resources.postMCP("fixture", "after move"); err != nil {
		t.Fatal(err)
	}
	once.Do(func() { close(release) })
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("call did not finish")
	}
	if initializes.Load() != 1 || resources.Jobs != originalJobs || resources.MCP != originalRef {
		t.Fatalf("identity changed: initializes=%d", initializes.Load())
	}
	if k.Bot.Name() != "after" || k.Inbox != resources.box || k.Inbox == stale.Inbox {
		t.Fatal("invoking Kernel Bot/Inbox did not follow move")
	}
	job, err := resources.Jobs.Status(receipt.Job.ID)
	if err != nil || !job.Notified || job.Status != "done" || job.Output != filepath.Join(bot.Dir(root, "after"), "shell-jobs", job.ID+".log") {
		t.Fatalf("moved completion: %+v %v", job, err)
	}
	if err := resources.box.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := len(resources.box.Pending()); got != 3 {
		t.Fatalf("want two notes and one completion, got %d", got)
	}
	if _, err := (profileTool{stale}).Run(ctx, json.RawMessage(`{"description":"current resource path"}`)); err != nil {
		t.Fatal(err)
	}
	if stale.Bot.Name() != "after" {
		t.Fatal("legacy profile did not resolve current resource")
	}
	if _, err := os.Stat(bot.Dir(root, "before")); !os.IsNotExist(err) {
		t.Fatalf("old directory recreated: %v", err)
	}
	if coding.Record().Dir != filepath.Join(bot.Dir(root, "after"), "code") {
		t.Fatal("coding mover did not follow")
	}
}

func TestMCP2aResourceDeleteFencesLateDelivery(t *testing.T) {
	root := t.TempDir()
	if _, err := bot.Create(root, "keeper", "fixture", nil); err != nil {
		t.Fatal(err)
	}
	b, err := bot.Create(root, "target", "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := mcp.NewManager(ctx, mcp.Options{})
	defer manager.Close()
	r, err := NewBotResources(ctx, b, manager)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	finished := make(chan struct{})
	receipt := r.Jobs.RunCall(ctx, tools.CallRequest{Name: "fixture", Background: true, OnDone: func(tools.Job) {},
		Prepare: func(ctx context.Context, _ string) (context.Context, func()) { return ctx, func() { close(finished) } },
		Run:     func(context.Context) tools.CallResult { return tools.CallResult{Text: "done"} },
	})
	if receipt.Job == nil {
		t.Fatalf("start: %+v", receipt)
	}
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("call did not finish")
	}
	if err := r.BeginDelete("target"); err != nil {
		t.Fatal(err)
	}
	r.Jobs.Shutdown()
	// Hold the real Jobs meta critical section across the delivery callback,
	// and prove FinishDelete cannot pass its final removal fence yet.
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	metaDone := make(chan error, 1)
	go func() {
		metaDone <- r.Jobs.Deliver(receipt.Job.ID, func(job tools.Job, _ tools.CallResult) error {
			r.route.RLock()
			err := r.box.Post(link.NewEvent("shell", tools.JobEventType, job.Result, job.ID, time.Now()))
			r.route.RUnlock()
			close(entered)
			<-release // actual send returned its route lock, ack write is still pending
			return err
		})
	}()
	<-entered
	removed := make(chan error, 1)
	go func() {
		ok, err := r.FinishDelete("target")
		if !ok && err == nil {
			err = fmt.Errorf("not removed")
		}
		removed <- err
	}()
	select {
	case err := <-removed:
		t.Fatalf("delete crossed metadata fence: %v", err)
	default:
	}
	if err := r.postMCP("fixture", "during final wait"); err != nil {
		t.Fatal(err)
	}
	once.Do(func() { close(release) })
	if err := <-metaDone; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-removed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("delete stuck")
	}
	r.Close()
	if err := r.postMCP("fixture", "too late"); err == nil {
		t.Fatal("post-delete note admitted")
	}
	r.deliverToolDone(*receipt.Job)
	if res := r.Jobs.RunCall(ctx, tools.CallRequest{Name: "late", Run: func(context.Context) tools.CallResult { t.Error("late call ran"); return tools.CallResult{} }}); res.Err == nil {
		t.Fatal("late call admitted")
	}
	if _, _, _, err := r.TryCurrentHandle(); err == nil {
		t.Fatal("closed wake admitted")
	}
	if _, err := os.Stat(bot.Dir(root, "target")); !os.IsNotExist(err) {
		t.Fatalf("deleted directory recreated: %v", err)
	}
}
