package exec

import (
	"os"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/provider"
)

func TestTranscriptWindowBoundsAndExclusiveBefore(t *testing.T) {
	dir := t.TempDir()
	j := &job{dir: dir}
	for i := 0; i < 205; i++ {
		if _, err := j.record(SeqEntry{Entry: bot.Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("row")}}}); err != nil {
			t.Fatal(err)
		}
	}
	w, err := ReadTranscript(dir, 0, 500)
	if err != nil || len(w.Entries) != 200 || !w.HasMore || w.NextBefore != 6 || w.Entries[199].Seq != 205 {
		t.Fatalf("window %+v err %v", w, err)
	}
	w, err = ReadTranscript(dir, 6, 40)
	if err != nil || len(w.Entries) != 5 || w.HasMore || w.Entries[4].Seq != 5 {
		t.Fatalf("before %+v err %v", w, err)
	}
	// The reader must not inspect corrupt data after the exclusive boundary.
	f, err := os.OpenFile(transcriptPath(dir), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("not-json\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := ReadTranscript(dir, 6, 40); err != nil {
		t.Fatalf("read beyond before: %v", err)
	}
}

func TestTranscriptWindowLargeLineAndPartialTail(t *testing.T) {
	dir := t.TempDir()
	j := &job{dir: dir}
	full := strings.Repeat("private-tool-detail", 20000)
	j.record(SeqEntry{Entry: bot.Entry{Message: provider.Message{Role: provider.RoleTool, Content: provider.ToolResult("call", full)}}})
	j.record(SeqEntry{Entry: bot.Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("tail")}}})
	f, err := os.OpenFile(transcriptPath(dir), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"message":`)
	f.Close()
	w, err := ReadTranscript(dir, 0, 40)
	if err != nil || len(w.Entries) != 2 || !w.Entries[0].Truncated || w.Entries[1].Seq != 2 {
		t.Fatalf("large window %+v %v", w, err)
	}
	if strings.Contains(w.Entries[0].Message.Text(), "private-tool-detail") {
		t.Fatal("public oversized placeholder exposed raw JSON")
	}
	original, err := readRecords(dir)
	if err != nil || original[0].Message.Text() != full {
		t.Fatal("public window altered complete original", err)
	}
}
