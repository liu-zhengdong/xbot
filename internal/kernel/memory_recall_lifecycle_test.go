package kernel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/cliops"
	"xbot/internal/config"
	"xbot/internal/fsutil"
	"xbot/internal/tools"
	"xbot/internal/work"
)

func TestMemoryRecallRuntimeScopeLockHoldsCurrentRouteThroughProfileRename(t *testing.T) {
	k, endpoint := memoryRecallRuntimeFixture(t)
	b := k.Bot
	k.ensureTurn().turnSender = "u1"
	memoryRecallWrite(t, b.Root, "bots", "source", "free.txt", "needle BOT-BEFORE-RENAME")
	personPath := memoryRecallWrite(t, b.Root, "people", "u1", "free.txt", "needle PERSON-STABLE")
	personBefore, err := os.Stat(personPath)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := fsutil.Lock(filepath.Join(bot.Dir(b.Root, "source"), "memory", ".lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	if err := os.MkdirAll(k.WorkDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(k.WorkDir, "sentinel"), []byte("workspace follows rename"), 0600); err != nil {
		t.Fatal(err)
	}
	runRoutineSource(t, k, nil, `{"command":"memory-recall --self --query needle"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		type result struct {
			reply cliops.MemoryRecallReply
			err   error
		}
		scanned := make(chan result, 1)
		go func() {
			reply, err := cliops.CallMemoryRecall(ctx, endpoint, b.Root, tools.CallID(ctx), cliops.MemoryRecall{Query: "needle"})
			scanned <- result{reply, err}
		}()
		// The only new route reader is this actual scan, blocked at its real
		// scope flock. Observe that reader without any production test hook.
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			if !k.Resources.route.TryLock() {
				break
			}
			k.Resources.route.Unlock()
			select {
			case got := <-scanned:
				t.Fatalf("scan bypassed held scope lock: %+v", got)
			case <-deadline.C:
				t.Fatal("scan never acquired current route before waiting on scope lock")
			case <-tick.C:
			}
		}
		type renamed struct {
			reply cliops.Profile
			err   error
		}
		moved := make(chan renamed, 1)
		go func() {
			name := "renamed"
			reply, err := cliops.Call(ctx, endpoint, b.Root, &cliops.Update{Name: &name})
			moved <- renamed{reply, err}
		}()
		select {
		case got := <-moved:
			t.Fatalf("legitimate profile rename crossed the held scan route: %+v", got)
		case <-time.After(60 * time.Millisecond):
		}
		if err := lock.Release(); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-scanned:
			if got.err != nil || !strings.Contains(got.reply.Text, "BOT-BEFORE-RENAME") || !strings.Contains(got.reply.Text, "PERSON-STABLE") {
				t.Errorf("complete original union lost across scope-lock rename: %+v", got)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("scan failed to release original route after scope unlock")
		}
		select {
		case got := <-moved:
			if got.err != nil || !got.reply.Moved || got.reply.Name != "renamed" {
				t.Errorf("profile rename result %+v", got)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("profile rename failed to finish after short IO released")
		}
		// This same listener now resolves the new Bot directory.
		reply, err := cliops.CallMemoryRecall(ctx, endpoint, b.Root, tools.CallID(ctx), cliops.MemoryRecall{Query: "needle"})
		if err != nil || !strings.Contains(reply.Text, "BOT-BEFORE-RENAME") || !strings.Contains(reply.Text, "PERSON-STABLE") {
			t.Errorf("same source after profile rename: %+v %v", reply, err)
		}
		return "completed", nil
	})
	personAfter, err := os.Stat(personPath)
	if err != nil || !os.SameFile(personBefore, personAfter) {
		t.Fatalf("Bot rename moved person store inode: %v", err)
	}
	if body, err := os.ReadFile(filepath.Join(work.BotDir(b.Root, "renamed"), "sentinel")); err != nil || string(body) != "workspace follows rename" {
		t.Fatalf("original workspace was not moved with source: %q %v", body, err)
	}
	for _, path := range []string{bot.Dir(b.Root, "source"), work.BotDir(b.Root, "source")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("scan revived former source path %s: %v", path, err)
		}
	}
}

func memoryRecallOverflowFiles(t *testing.T, k *Kernel, character string, depth, width int) {
	t.Helper()
	components := make([]string, depth)
	for i := range components {
		components[i] = strings.Repeat(character, width) + fmt.Sprintf("%d", i)
	}
	prefix := strings.Join(components, "/")
	for i := 0; i < 20; i++ {
		body := "needle SOURCE_SECRET held-recall-secret-12345 STAGED_SECRET RESOURCE_ONLY_SECRET " + strings.Repeat(character, 230) + " TAIL-OUTSIDE-ORIGINAL-200-RUNES"
		memoryRecallWrite(t, k.Bot.Root, "bots", "source", fmt.Sprintf("%s/%02d.txt", prefix, i), body)
	}
}

func TestMemoryRecallRuntimeFullTextArtifactRedactionCurrentWorkspace(t *testing.T) {
	for _, tc := range []struct {
		name, character string
		depth, width    int
		humanOverflow   bool
	}{
		{"escaped_control", "\x01", 3, 70, false},
		{"deep_unicode", "界", 5, 30, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k, endpoint := memoryRecallRuntimeFixture(t)
			const heldName = "XBOT_MEMORY_RECALL_TEST_SECRET"
			previous, _ := config.Held(heldName)
			config.SetHeld(heldName, "held-recall-secret-12345")
			t.Cleanup(func() { config.SetHeld(heldName, previous) })
			k.StageSecrets("recall-test", map[string]string{"secret": "STAGED_SECRET"})
			resourceReplacement := "[resource]"
			if tc.humanOverflow {
				resourceReplacement += strings.Repeat("界", 260)
			}
			k.Resources.Redact = func(text string) string { return strings.ReplaceAll(text, "RESOURCE_ONLY_SECRET", resourceReplacement) }
			k.Redact = func(text string) string { return strings.ReplaceAll(text, "SOURCE_SECRET", "[source]") }
			memoryRecallOverflowFiles(t, k, tc.character, tc.depth, tc.width)
			runRoutineSource(t, k, nil, `{"command":"memory-recall --self --query needle"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
				original, err := k.recall(ctx, recallInput{Query: "needle"})
				if err != nil {
					t.Error(err)
					return "", err
				}
				want := scrubSecrets(k.redactor()(original), k.secretValues())
				full := cliops.MemoryRecallReply{Text: want}
				wantJSON, err := json.Marshal(full)
				if err != nil {
					t.Error(err)
					return "", err
				}
				wantJSON = append(wantJSON, '\n')
				if len(want) <= 6<<10 || len(wantJSON) <= tools.DefaultShellOutputBytes || (len(cliops.FormatMemoryRecall(full)) > tools.DefaultShellOutputBytes) != tc.humanOverflow || strings.Count(want, "- bot:") != 20 {
					t.Errorf("overflow fixture text=%d json=%d human=%d hits=%d", len(want), len(wantJSON), len(cliops.FormatMemoryRecall(full)), strings.Count(want, "- bot:"))
					return "", fmt.Errorf("overflow fixture precondition failed")
				}
				// The scanner has released its route when result redaction runs.
				// Use this existing dependency to perform a legitimate profile
				// rename at the exact scan -> artifact boundary.
				var once sync.Once
				var renameErr error
				k.Redact = func(text string) string {
					once.Do(func() {
						name := "renamed"
						_, renameErr = k.ApplyProfileChange(bot.ProfileChange{Name: &name})
					})
					return strings.ReplaceAll(text, "SOURCE_SECRET", "[source]")
				}
				reply, err := cliops.CallMemoryRecall(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.MemoryRecall{Query: "needle"})
				if renameErr != nil || err != nil || reply.File == "" || reply.Access != "local" || reply.Bytes != int64(len(wantJSON)) || reply.Text != "" || reply.Error != "" {
					t.Errorf("current artifact metadata %+v err=%v rename=%v", reply, err, renameErr)
					return "", fmt.Errorf("current artifact metadata mismatch")
				}
				memoryRecallReceipt(t, reply)
				path := filepath.Join(work.BotDir(k.Bot.Root, "renamed"), filepath.FromSlash(reply.File))
				raw, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(raw, wantJSON) {
					t.Errorf("full original twenty-hit Text JSON differs bytes=%d/%d err=%v", len(raw), len(wantJSON), err)
					return "", fmt.Errorf("full original twenty-hit Text JSON mismatch")
				}
				for _, secret := range []string{"SOURCE_SECRET", "held-recall-secret-12345", "STAGED_SECRET", "RESOURCE_ONLY_SECRET"} {
					if strings.Contains(string(raw), secret) {
						t.Errorf("artifact leaked held source secret %q", secret)
					}
				}
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != 0600 || info.Size() > work.ResultMaxBytes {
					t.Errorf("artifact mode/limit info=%v err=%v", info, err)
					return "", fmt.Errorf("artifact mode/limit mismatch")
				}
				for _, old := range []string{bot.Dir(k.Bot.Root, "source"), work.BotDir(k.Bot.Root, "source")} {
					if _, err := os.Stat(old); !os.IsNotExist(err) {
						t.Errorf("artifact revived old source path %s: %v", old, err)
					}
				}
				return "completed", nil
			})
		})
	}
}

func TestMemoryRecallRuntimeArtifactFilesystemFaultHasNoFalseReference(t *testing.T) {
	k, endpoint := memoryRecallRuntimeFixture(t)
	memoryRecallOverflowFiles(t, k, "\x02", 3, 70)
	if err := os.MkdirAll(k.WorkDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(k.WorkDir, work.ResultDir), []byte("block result directory"), 0600); err != nil {
		t.Fatal(err)
	}
	runRoutineSource(t, k, nil, `{"command":"memory-recall --self --query needle"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		reply, err := cliops.CallMemoryRecall(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.MemoryRecall{Query: "needle"})
		if err == nil || reply.File != "" || reply.Bytes != 0 || reply.Access != "" || reply.Text != "" || !strings.Contains(reply.Error, "write recall result") {
			t.Errorf("real filesystem fault returned false artifact: %+v %v", reply, err)
		}
		memoryRecallReceipt(t, reply)
		return "completed", nil
	})
}

func TestMemoryRecallRuntimeErrorDisplayOnlyBoundAndRedaction(t *testing.T) {
	k, endpoint := memoryRecallRuntimeFixture(t)
	k.StageSecrets("recall-error", map[string]string{"error": "ERROR_SECRET"})
	k.Redact = func(string) string { return "[source] ERROR_SECRET " + strings.Repeat("\x01", 20000) }
	runRoutineSource(t, k, nil, `{"command":"memory-recall --self --scope invalid"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		reply, err := cliops.CallMemoryRecall(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.MemoryRecall{Scope: "invalid"})
		if err == nil || reply.Error == "" || strings.Contains(reply.Error, "ERROR_SECRET") || !strings.Contains(reply.Error, "[source] [secret]") || reply.Text != "" || reply.File != "" {
			t.Errorf("display error redaction/bound: text=%d error=%d err=%v", len(reply.Text), len(reply.Error), err)
		}
		memoryRecallReceipt(t, reply)
		return "completed", nil
	})
}
