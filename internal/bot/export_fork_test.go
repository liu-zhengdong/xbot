package bot

import (
	"bufio"
	"encoding/json"
	"strings"
	"testing"

	"xbot/internal/provider"
)

func TestExportForkMarkdownAndJSONL(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Append(mainRow("start")); err != nil {
		t.Fatal(err)
	}
	if err := b.Append(Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("forked work")}, Branch: "bg_7"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AppendFork("bg_7", 1, "long job"); err != nil {
		t.Fatal(err)
	}
	if err := b.Append(Entry{Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("after marker")}, Branch: "bg_7"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AppendForkEnd("bg_7", ForkDone, 2); err != nil {
		t.Fatal(err)
	}

	md, err := ExportTranscript(b, TranscriptExport{Format: "md"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "[后台 bg_7]") {
		t.Fatalf("branch heading missing\n%s", md)
	}
	if !strings.Contains(md, "后台任务开始 [bg_7] seq 3 from 1: long job") {
		t.Fatalf("fork line missing\n%s", md)
	}
	if !strings.Contains(md, "后台任务结束 [bg_7] seq 5 status done steps 2") {
		t.Fatalf("fork end line missing\n%s", md)
	}

	raw, err := ExportTranscript(b, TranscriptExport{Format: "jsonl", Tools: true})
	if err != nil {
		t.Fatal(err)
	}
	bySeq := map[int]Entry{}
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		var row exportEntry
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		bySeq[row.Seq] = Entry{Message: row.Message, Branch: row.Branch, Mark: row.Mark}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if bySeq[2].Branch != "bg_7" || bySeq[4].Branch != "bg_7" {
		t.Fatalf("branch lost %+v %+v", bySeq[2], bySeq[4])
	}
	if bySeq[3].Mark == nil || bySeq[3].Mark.Kind != ForkKindStart || bySeq[3].Mark.From != 1 || bySeq[3].Mark.Task != "long job" {
		t.Fatalf("fork mark lost %+v", bySeq[3].Mark)
	}
	if bySeq[5].Mark == nil || bySeq[5].Mark.Kind != ForkKindEnd || bySeq[5].Mark.Status != ForkDone || bySeq[5].Mark.Steps != 2 {
		t.Fatalf("fork end lost %+v", bySeq[5].Mark)
	}
}

func TestOldTranscriptReadsWithoutBranch(t *testing.T) {
	var e Entry
	if err := json.Unmarshal([]byte(`{"time":"2026-10-15T12:00:00Z","message":{"role":"user","content":"hi"}}`), &e); err != nil {
		t.Fatal(err)
	}
	if e.Branch != "" || e.Mark != nil {
		t.Fatalf("old row gained fields %+v", e)
	}
}
