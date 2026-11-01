package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/code"
	"xbot/internal/exec"
	"xbot/internal/provider"
)

// A serve pass closes its parent kernel after Drain, while these two existing
// execution backends continue. Their actual Shell processes still need self.
func TestRetiredRuntimeFollowsLocalTaskLifetime(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture")
	}
	for _, kind := range []string{"subagent", "coding"} {
		t.Run(kind, func(t *testing.T) {
			k, _, _ := freshBot(t)
			k.WorkDir = t.TempDir()
			k.Dispatch, k.Permissive, k.AllowBackground = true, true, true
			ready, release := make(chan struct{}), make(chan struct{})
			nextReady, nextRelease := make(chan struct{}), make(chan struct{})
			var releaseOnce, nextReleaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			defer nextReleaseOnce.Do(func() { close(nextRelease) })
			checked := make(chan error, 2)
			var steps [2]atomic.Int32
			var endpoint string
			p := providerFunc(func(ctx context.Context, req provider.Request, _ func(string)) (provider.Message, error) {
				index := 0
				if len(req.Messages) > 1 && strings.Contains(req.Messages[1].Text(), "queued successor") {
					index = 1
				}
				if steps[index].Add(1) == 1 {
					gate := release
					if index == 0 {
						close(ready)
					} else {
						close(nextReady)
						gate = nextRelease
					}
					select {
					case <-gate:
					case <-ctx.Done():
						return provider.Message{}, ctx.Err()
					}
					args, _ := json.Marshal(map[string]string{"description": "inspect retained runtime identity", "command": `printf '%s\n' "$XBOT_BIN" "$XBOT_ENDPOINT" "$XBOT_BOT"`})
					return provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{{ID: "identity", Type: "function", Function: provider.FunctionCall{Name: "sh", Arguments: string(args)}}})}, nil
				}
				var found bool
				for _, m := range req.Messages {
					if m.Role == provider.RoleTool && strings.Contains(m.Text(), endpoint) && strings.Contains(m.Text(), k.Bot.Name()) {
						found = true
					}
				}
				if !found {
					checked <- fmt.Errorf("%s shell lost runtime identity", kind)
				} else {
					checked <- nil
				}
				return provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("done")}, nil
			})
			k.Provider = p
			var coding *code.Link
			if kind == "coding" {
				coding = code.Open(bot.Dir(k.Bot.Root, k.Bot.Name()), k.Bot.InboxPath())
				coding.UseProvider(p)
				k.Links = append(k.Links, coding)
			}
			if err := k.StartOperations(); err != nil {
				t.Fatal(err)
			}
			defer k.CloseOperations()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			k.BindOperationsContext(ctx)
			endpoint = operationEndpoint(k)
			if kind == "subagent" {
				id, _ := newAgentID()
				_, err := k.ensureExec().Start(ctx, exec.StartRequest{ID: id, Bot: k.Bot.Name(), Task: "inspect self", Mode: exec.ModeBrief, Dir: k.Bot.AgentDir(id), Type: "dev", TimeoutSec: 5})
				if err != nil {
					t.Fatal(err)
				}
			} else {
				args, _ := json.Marshal(map[string]any{"work_dir": k.WorkDir, "goal": "inspect self", "max_steps": 3, "timeout_sec": 5})
				for _, tool := range coding.Tools() {
					if tool.Name() == "code_start" {
						if _, err := tool.Run(ctx, args); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			select {
			case <-ready:
			case <-time.After(2 * time.Second):
				t.Fatal("task did not enter model gate")
			}
			if kind == "coding" {
				args, _ := json.Marshal(map[string]any{"work_dir": k.WorkDir, "goal": "queued successor", "max_steps": 3, "timeout_sec": 5})
				for _, tool := range coding.Tools() {
					if tool.Name() == "code_start" {
						out, err := tool.Run(ctx, args)
						if err != nil || !strings.HasPrefix(out, "queued ") {
							t.Fatalf("second task did not queue: %q %v", out, err)
						}
					}
				}
			}
			if err := k.Close(); err != nil {
				t.Fatal(err)
			}
			desc := "parent pass already returned"
			if _, err := cliops.Call(ctx, endpoint, k.Bot.Root, &cliops.Update{Description: &desc}); err != nil {
				t.Fatalf("live child lost endpoint after parent Close: %v", err)
			}
			releaseOnce.Do(func() { close(release) })
			select {
			case err := <-checked:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("child shell did not finish")
			}
			if kind == "coding" {
				select {
				case <-nextReady:
				case <-time.After(2 * time.Second):
					t.Fatal("queued successor did not start")
				}
				// Retirement may run concurrently with a code queue handoff.
				k.retireOperations()
				if _, err := cliops.Call(ctx, endpoint, k.Bot.Root, nil); err != nil {
					t.Fatalf("queue handoff lost endpoint: %v", err)
				}
				nextReleaseOnce.Do(func() { close(nextRelease) })
				select {
				case err := <-checked:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("queued successor shell did not finish")
				}
			}
			deadline := time.NewTimer(2 * time.Second)
			defer deadline.Stop()
			tick := time.NewTicker(5 * time.Millisecond)
			defer tick.Stop()
			for {
				if _, err := cliops.Call(ctx, endpoint, k.Bot.Root, nil); err != nil {
					break
				}
				select {
				case <-tick.C:
				case <-deadline.C:
					t.Fatal("idle child retained endpoint")
				}
			}
		})
	}
}
