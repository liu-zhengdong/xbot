package kernel

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/tools"
)

// This unit exercises the actual RunCall Prepare/cleanup boundary. The
// independent binary fixture owns the complete interpreter/CLI script journey.
func TestSourceRuntimeLeaseCoversInterpreterCall(t *testing.T) {
	k, _, _ := freshBot(t)
	k.WorkDir = t.TempDir()
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	defer k.CloseOperations()
	endpoint := operationEndpoint(k)
	jobs := k.shellJobs()
	defer jobs.Shutdown()
	other, err := bot.Load(k.Bot.Root, k.Bot.Name())
	if err != nil {
		t.Fatal(err)
	}
	idle := &Kernel{Bot: other, shell: jobs}
	if err := idle.StartOperations(); err != nil {
		t.Fatal(err)
	}
	defer idle.CloseOperations()
	idleEndpoint := operationEndpoint(idle)
	ready, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	results := make(chan error, 1)
	var notifications atomic.Int32
	jobs.OnDone = func(tools.Job) { notifications.Add(1) }
	receipt := k.runToolCall(context.Background(), leaseInterpreter{}, nil, func(ctx context.Context) tools.CallResult {
		if _, err := cliops.Call(ctx, endpoint, k.Bot.Root, nil); err != nil {
			results <- err
			close(ready)
			return tools.CallResult{Err: err}
		}
		close(ready)
		// Deliberately model a call which has not returned after cancellation.
		// Requested cancel must not release its listener before actual cleanup.
		<-release
		_, err := cliops.Call(context.Background(), endpoint, k.Bot.Root, nil)
		results <- err
		return tools.CallResult{Text: "two profile protocol calls completed", Err: err}
	})
	if receipt.Job == nil {
		t.Fatalf("missing background receipt: %+v", receipt)
	}
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("call did not enter gate")
	}
	idle.retireOperations()
	if _, err := cliops.Call(context.Background(), idleEndpoint, k.Bot.Root, nil); err == nil {
		t.Fatal("idle source retained another kernel's call")
	}
	k.retireOperations()
	if _, err := cliops.Call(context.Background(), endpoint, k.Bot.Root, nil); err != nil {
		t.Fatalf("retirement closed live interpreter endpoint: %v", err)
	}
	if _, err := jobs.Cancel(receipt.Job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := cliops.Call(context.Background(), endpoint, k.Bot.Root, nil); err != nil {
		t.Fatalf("requested cancel closed call before it returned: %v", err)
	}
	once.Do(func() { close(release) })
	select {
	case err := <-results:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("second protocol call did not finish")
	}
	jobs.Shutdown() // joins actual cleanup/done; release must not wait on itself
	if notifications.Load() != 1 {
		t.Fatalf("completion callback count=%d", notifications.Load())
	}
	if _, err := cliops.Call(context.Background(), endpoint, k.Bot.Root, nil); err == nil {
		t.Fatal("completed source retained its listener")
	}
}

type leaseInterpreter struct{}

func (leaseInterpreter) Confirm(json.RawMessage) (bool, string, error) { return false, "lease", nil }

func (leaseInterpreter) Name() string                                         { return "sh" }
func (leaseInterpreter) Description() string                                  { return "lease fixture interpreter" }
func (leaseInterpreter) Parameters() any                                      { return map[string]any{"type": "object"} }
func (leaseInterpreter) Run(context.Context, json.RawMessage) (string, error) { return "", nil }
func (leaseInterpreter) WaitPolicy(json.RawMessage) (time.Duration, bool) {
	return time.Millisecond, true
}
