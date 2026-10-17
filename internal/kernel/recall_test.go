package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"xbot/internal/fsutil"
	"xbot/internal/memory"
)

func TestRecallFolder(t *testing.T) {
	b := testBot(t)
	dir := filepath.Join(b.Root, "bots", b.Name(), "memory")
	for path, body := range map[string]string{
		"memory.md":      "# memory\n## 索引\n（暂无）\n## 条目\n- (2026-10-18) needle current\n- (2026-09-01) needle stale\n",
		"log/2026-10.md": "- 2026-10-01 10:00 [对话] needle oldlog\n- 2026-10-18 10:00 [对话] needle newlog\n",
		"projects/x.md":  "# Project\nneedle launch violet-orbit-73\n" + strings.Repeat("needle extra\n", 22),
		"ignored.bak.md": "needle backup", "binary.bin": "needle binary",
	} {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "projects/x.md"), filepath.Join(dir, "linked.md")); err != nil {
		t.Fatal(err)
	}
	tool := recallTool{&Kernel{Bot: b, Memory: memory.Open(b.Root)}}
	run := func(args string) string {
		t.Helper()
		got, err := tool.Run(context.Background(), json.RawMessage(args))
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	got := run(`{"query":"launch","scope":"bot"}`)
	if !strings.Contains(got, "bot:projects/x.md:2:") || strings.Contains(got, "linked") {
		t.Fatal(got)
	}
	got = run(`{"query":"needle"}`)
	if strings.Count(got, "- bot:") != 20 || !strings.Contains(got, "(7 more;") || strings.Contains(got, "ignored") || strings.Contains(got, "binary") {
		t.Fatal(got)
	}
	got = run(`{"query":"oldlog newlog current stale","since":"2026-10-15"}`)
	if !strings.Contains(got, "newlog") || !strings.Contains(got, "current") || strings.Contains(got, "oldlog") || strings.Contains(got, "stale") {
		t.Fatal(got)
	}
	if got := run(`{"query":"absent"}`); got != "no matches" {
		t.Fatal(got)
	}
}

func TestRecallIsolationAndBudgets(t *testing.T) {
	for _, kind := range []string{"ancestor", "files", "bytes", "lock"} {
		t.Run(kind, func(t *testing.T) {
			b := testBot(t)
			dir := filepath.Join(b.Root, "bots", b.Name(), "memory")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			write := func(path, body string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			write(filepath.Join(dir, "x.md"), "needle")
			if kind == "ancestor" {
				parent := filepath.Dir(dir)
				if err := os.Rename(parent, parent+"-other"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(parent+"-other", parent); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "files" || kind == "bytes" {
				n, body := 2001, "needle"
				if kind == "bytes" {
					n, body = 17, strings.Repeat("z", 256<<10)
				}
				for i := 0; i < n; i++ {
					write(filepath.Join(dir, fmt.Sprintf("%04d.txt", i)), body)
				}
			}
			var lock *fsutil.FileLock
			if kind == "lock" {
				var err error
				lock, err = fsutil.Lock(filepath.Join(dir, ".lock"))
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Release()
			}
			result := make(chan string, 1)
			go func() {
				got, err := (recallTool{&Kernel{Bot: b}}).Run(context.Background(), json.RawMessage(`{"query":"needle","scope":"bot"}`))
				if err != nil {
					got = err.Error()
				}
				result <- got
			}()
			if lock != nil {
				select {
				case got := <-result:
					t.Fatalf("read while locked: %s", got)
				case <-time.After(50 * time.Millisecond):
				}
				write(filepath.Join(dir, "x.md"), "restored")
				lock.Release()
			}
			got := <-result
			switch kind {
			case "ancestor", "lock":
				if got != "no matches" {
					t.Fatal(got)
				}
			default:
				if strings.Contains(got, "bot:x.md:") || kind == "files" && !strings.Contains(got, "(1980 more;") {
					t.Fatal(got)
				}
				if !strings.Contains(got, "(search incomplete: file/byte limit reached)") {
					t.Fatal(got)
				}
			}
		})
	}
}
