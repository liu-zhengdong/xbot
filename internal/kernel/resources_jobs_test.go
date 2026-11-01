package kernel

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/mcp"
	"xbot/internal/tools"
)

func TestSharedResourcesKeepSourceCallAndOriginalCompletion(t *testing.T) {
	k, _, _ := freshBot(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := mcp.NewManager(ctx, mcp.Options{})
	defer manager.Close()
	resources, err := NewBotResources(ctx, k.Bot, manager)
	if err != nil {
		t.Fatal(err)
	}
	defer resources.Close()
	k.Resources = resources
	k.WorkDir = t.TempDir()
	k.Redact = func(text string) string { return strings.ReplaceAll(text, "source-a-key", "[source-a]") }
	originalDefault := resources.Jobs.Redact("source-a-key")
	release := make(chan struct{})
	cancelled := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	receipt := k.runToolCall(ctx, resourceCallTool{}, nil, func(ctx context.Context) tools.CallResult {
		<-ctx.Done()
		close(cancelled)
		<-release
		return tools.CallResult{Text: "result source-a-key"}
	})
	if receipt.Job == nil {
		t.Fatalf("no background receipt: %+v", receipt)
	}
	if err := k.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
		t.Fatal("borrowing kernel closed the source call")
	default:
	}
	b, err := bot.Load(k.Bot.Root, k.Bot.Name())
	if err != nil {
		t.Fatal(err)
	}
	next := &Kernel{Bot: b, Resources: resources, Inbox: resources.box, WorkDir: t.TempDir(), Redact: func(text string) string { return strings.ReplaceAll(text, "source-b-key", "[source-b]") }}
	if next.shellJobs() != k.liveShell() {
		t.Fatal("new kernel replaced original Jobs")
	}
	if got := resources.Jobs.Redact("source-a-key"); got != originalDefault {
		t.Fatal("source kernel replaced shared default redactor")
	}
	if _, err := next.shellJobs().Cancel(receipt.Job.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("later kernel did not cancel original call")
	}
	job, err := resources.Jobs.Status(receipt.Job.ID)
	if err != nil || job.Status != "running" {
		t.Fatalf("requested cancellation settled actual call: %+v %v", job, err)
	}
	once.Do(func() { close(release) })
	resources.Jobs.Shutdown()
	job, err = resources.Jobs.Status(receipt.Job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(job.Result, "source-a-key") || !strings.Contains(job.Result, "[source-a]") {
		t.Fatalf("completion lost source redactor: %+v", job)
	}
	next.flushToolJobs()
	count := 0
	for _, event := range resources.box.Pending() {
		if event.ID == "shelljob_"+job.ID {
			count++
			if !event.Untrusted {
				t.Fatal("completion lost untrusted boundary")
			}
		}
	}
	if count != 1 {
		t.Fatalf("default completion count=%d", count)
	}
}

type resourceCallTool struct{ leaseInterpreter }

func (resourceCallTool) Name() string { return "resource_call_fixture" }
