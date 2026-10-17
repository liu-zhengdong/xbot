package kernel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
