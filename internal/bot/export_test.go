package bot

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"xbot/internal/provider"
)

func TestExportMarkdownAndJSONL(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	rows := []Entry{
		{Time: when, Message: provider.Message{Role: provider.RoleUser, Content: "hello seq one"}},
		{Time: when.Add(time.Minute), Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Type: "function", Function: provider.FunctionCall{Name: "shell", Arguments: `{"command":"ls"}`},
		}}}},
		{Time: when.Add(2 * time.Minute), Message: provider.Message{Role: provider.RoleTool, ToolCallID: "c1", Content: "TOOL-RESULT"}},
		{Time: when.Add(3 * time.Minute), Message: provider.Message{Role: provider.RoleAssistant, Content: "done seq four"}},
	}
	for _, e := range rows {
		if err := b.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.SaveState(State{Blocks: []SummaryBlock{{
		From: 1, To: 3, Text: "SUMMARY-TEXT", Focus: "keep the greeting", At: when,
	}}}); err != nil {
		t.Fatal(err)
	}
	loc := time.FixedZone("CST", 8*3600)
	md, err := ExportTranscript(b, TranscriptExport{Format: "md", Loc: loc})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "## Summary seq 1-3") || !strings.Contains(md, "SUMMARY-TEXT") || !strings.Contains(md, "keep the greeting") {
		t.Fatalf("summary missing\n%s", md)
	}
	if !strings.Contains(md, "2026-10-15 20:00:00 CST") {
		t.Fatalf("local time missing\n%s", md)
	}
	if !strings.Contains(md, "## seq 1 ·") || !strings.Contains(md, "hello seq one") || !strings.Contains(md, "## seq 4 ·") {
		t.Fatalf("seqs missing\n%s", md)
	}
	if strings.Contains(md, "TOOL-RESULT") || strings.Contains(md, "shell") {
		t.Fatalf("tools leaked into default md\n%s", md)
	}
	withTools, err := ExportTranscript(b, TranscriptExport{Format: "md", Tools: true, Loc: loc})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(withTools, "TOOL-RESULT") || !strings.Contains(withTools, "tool shell") || !strings.Contains(withTools, "## seq 3 ·") {
		t.Fatalf("tools flag\n%s", withTools)
	}

	raw, err := ExportTranscript(b, TranscriptExport{Format: "jsonl", Tools: true})
	if err != nil {
		t.Fatal(err)
	}
	seqs := map[int]provider.Message{}
	var sawSummary bool
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		line := sc.Bytes()
		var kind struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(line, &kind); err != nil {
			t.Fatal(err)
		}
		if kind.Kind == "summary" {
			sawSummary = bytes.Contains(line, []byte("SUMMARY-TEXT"))
			continue
		}
		var row exportEntry
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatal(err)
		}
		var entry Entry
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatal(err)
		}
		if entry.Message.Content != row.Message.Content || entry.Message.Role != row.Message.Role {
			t.Fatalf("reimport %+v vs %+v", entry.Message, row.Message)
		}
		seqs[row.Seq] = row.Message
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if !sawSummary {
		t.Fatal("jsonl summary missing")
	}
	if len(seqs) != 4 || seqs[1].Content != "hello seq one" || seqs[4].Content != "done seq four" {
		t.Fatalf("seqs %+v", seqs)
	}
	if seqs[2].ToolCalls == nil || seqs[2].ToolCalls[0].Function.Name != "shell" || seqs[3].Content != "TOOL-RESULT" {
		t.Fatalf("tool rows %+v %+v", seqs[2], seqs[3])
	}
	quiet, err := ExportTranscript(b, TranscriptExport{Format: "jsonl"})
	if err != nil {
		t.Fatal(err)
	}
	quietSeq := map[int]bool{}
	sc = bufio.NewScanner(strings.NewReader(quiet))
	for sc.Scan() {
		var row exportEntry
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		if row.Seq == 0 {
			continue
		}
		quietSeq[row.Seq] = true
		if len(row.Message.ToolCalls) != 0 || row.Message.Role == provider.RoleTool {
			t.Fatalf("tools kept %+v", row)
		}
	}
	if quietSeq[2] || quietSeq[3] || !quietSeq[1] || !quietSeq[4] {
		t.Fatalf("filtered seqs %v", quietSeq)
	}
	if strings.Contains(quiet, "TOOL-RESULT") {
		t.Fatalf("tool text in jsonl\n%s", quiet)
	}
}

func TestExportRedacts(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Append(Entry{Time: time.Unix(10, 0).UTC(), Message: provider.Message{Role: provider.RoleUser, Content: "see SECRET-VALUE"}}); err != nil {
		t.Fatal(err)
	}
	redact := func(s string) string { return strings.ReplaceAll(s, "SECRET-VALUE", "[redacted]") }
	md, err := ExportTranscript(b, TranscriptExport{Format: "md", Loc: time.UTC, Redact: redact})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(md, "SECRET-VALUE") || !strings.Contains(md, "[redacted]") {
		t.Fatalf("md %s", md)
	}
	raw, err := ExportTranscript(b, TranscriptExport{Format: "jsonl", Redact: redact})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "SECRET-VALUE") || !strings.Contains(raw, "[redacted]") {
		t.Fatalf("jsonl %s", raw)
	}
}
