package bot

import (
	"os"
	"path/filepath"
	"testing"

	"xbot/internal/provider"
)

func TestWindowRangeReplayAndIndexUpgrade(t *testing.T) {
	b, err := Create(t.TempDir(), "reader", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer CloseStore(b)
	b.SetSegmentMax(300)
	for i := 0; i < 100; i++ {
		if err := b.Append(Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("unchanged original")}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.AppendFork("bg_old", 10, "legacy interval"); err != nil {
		t.Fatal(err)
	}
	call := provider.ToolCall{ID: "call", Type: "function"}
	call.Function.Name = "shell"
	call.Function.Arguments = `{}`
	if err := b.Append(Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.ToolContent([]provider.ToolCall{call})}}); err != nil {
		t.Fatal(err)
	}
	if err := b.Append(Entry{Message: provider.Message{Role: provider.RoleTool, Content: provider.ToolResult("call", "done")}}); err != nil {
		t.Fatal(err)
	}
	before, err := b.ConcatRaw()
	if err != nil {
		t.Fatal(err)
	}
	w, err := b.Window(WindowQuery{Around: 50, Limit: 3})
	if err != nil || len(w.Rows) != 3 || w.Rows[1].Seq != 50 {
		t.Fatalf("window %+v: %v", w, err)
	}
	for _, row := range w.Rows {
		if row.Branch != "bg_old" {
			t.Fatalf("outside marker lost branch: %+v", row)
		}
	}
	older, err := b.Window(WindowQuery{Before: w.HeadSeq, Limit: 3})
	if err != nil || older.TailSeq != w.HeadSeq-1 {
		t.Fatalf("older %+v %v", older, err)
	}
	newer, err := b.Window(WindowQuery{After: older.TailSeq, Limit: 3})
	if err != nil || newer.HeadSeq != w.HeadSeq || newer.TailSeq != w.TailSeq {
		t.Fatalf("newer %+v %v", newer, err)
	}
	// Simulate an interrupted derived-index upgrade: tables exist, but their
	// version is uncommitted and their contents incomplete. Reopen repairs it.
	s := b.store()
	if _, err := s.db.Exec(`DELETE FROM fork_spans; DELETE FROM tool_results; PRAGMA user_version=0`); err != nil {
		t.Fatal(err)
	}
	CloseStore(b)
	w, err = b.Window(WindowQuery{Around: 50, Limit: 1})
	if err != nil || w.Rows[0].Branch != "bg_old" {
		t.Fatalf("upgrade lost old fork: %+v %v", w, err)
	}
	result, found, err := b.ToolResult("call")
	if err != nil || !found || result.Message.Text() != "done" {
		t.Fatalf("result upgrade: %+v %v %v", result, found, err)
	}
	after, err := b.ConcatRaw()
	if err != nil || string(before) != string(after) {
		t.Fatal("window/index upgrade changed transcript")
	}
	// No full-history fallback after upgrade: the index still names a corrupt
	// sealed old segment, but windows covering other segments remain readable.
	files, err := b.SegmentFiles()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(files[0], 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(files[0], []byte("broken history\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Window(WindowQuery{}); err != nil {
		t.Fatalf("tail touched unrelated sealed history: %v", err)
	}
	if _, err := b.Window(WindowQuery{Around: 1, Limit: 1}); err == nil {
		t.Fatal("selected corruption not reported")
	}
	if _, err := os.Stat(filepath.Join(b.dir(), "transcript", "index.sqlite")); err != nil {
		t.Fatal(err)
	}
}
