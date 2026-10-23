package bot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/provider"
)

func TestSegmentRollBySizeAndDay(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	b.SetSegmentMax(120)
	day1 := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 10, 15, 1, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if err := b.Append(Entry{Time: day1, Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(strings.Repeat("a", 40))}}); err != nil {
			t.Fatal(err)
		}
	}
	files, err := b.SegmentFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 2 {
		t.Fatalf("size roll files %v", files)
	}
	var sealed []string
	for _, path := range files {
		if strings.Contains(filepath.Base(path), "-open") {
			continue
		}
		sealed = append(sealed, path)
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm()&0222 != 0 {
			t.Fatalf("sealed mode %v", fi.Mode())
		}
	}
	if len(sealed) == 0 {
		t.Fatal("no sealed segment")
	}
	before, err := os.ReadFile(sealed[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Append(Entry{Time: day2, Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("next day")}}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(sealed[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("sealed segment changed")
	}
	files, err = b.SegmentFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 3 {
		t.Fatalf("day roll files %v", files)
	}
	got, err := b.Entries()
	if err != nil || len(got) != 4 || got[3].Message.Text() != "next day" {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestMigrateLegacyJSONL(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(b.dir(), "transcript.jsonl")
	body := `{"time":"2026-10-15T12:00:00Z","event_id":"ev1","message":{"role":"user","content":"kept line"}}` + "\n"
	if err := os.WriteFile(legacy, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Message.Text() != "kept line" || got[0].EventID != "ev1" {
		t.Fatalf("%+v", got)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy still present: %v", err)
	}
	if _, err := os.Stat(legacy + ".migrated"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(b.dir(), "transcript", "READY")); err != nil {
		t.Fatal(err)
	}
	seq, err := b.FindEvent("ev1")
	if err != nil || seq != 1 {
		t.Fatalf("seq %d %v", seq, err)
	}
}

func TestCrashMidAppendAndMidIndex(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Append(Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("first line")}}); err != nil {
		t.Fatal(err)
	}
	files, err := b.SegmentFiles()
	if err != nil || len(files) != 1 {
		t.Fatalf("%v %v", files, err)
	}
	f, err := os.OpenFile(files[0], os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"message":{"role":"user","content":"PARTIAL"`); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	CloseStore(b)
	if err := b.Append(Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("second line")}}); err != nil {
		t.Fatal(err)
	}
	got, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Message.Text() != "first line" || got[1].Message.Text() != "second line" {
		t.Fatalf("%+v", got)
	}
	raw, err := b.ConcatRaw()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "PARTIAL") {
		t.Fatalf("partial line kept: %s", raw)
	}

	if err := b.appendSkipIndex(Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("indexed later")}}); err != nil {
		t.Fatal(err)
	}
	CloseStore(b)
	seqs, err := b.Search("indexed later", 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(seqs) != 1 || seqs[0] != 3 {
		t.Fatalf("seqs %v", seqs)
	}
}

func TestCorruptIndexRebuilds(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Append(Entry{EventID: "e", Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("durable text")}}); err != nil {
		t.Fatal(err)
	}
	CloseStore(b)
	dir := filepath.Join(b.dir(), "transcript")
	for _, name := range []string{"index.sqlite", "index.sqlite-wal", "index.sqlite-shm"} {
		_ = os.Remove(filepath.Join(dir, name))
	}
	if err := os.WriteFile(filepath.Join(dir, "index.sqlite"), []byte("not a database"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Message.Text() != "durable text" {
		t.Fatalf("%+v", got)
	}
	seqs, err := b.Search("durable", 2)
	if err != nil || len(seqs) != 1 || seqs[0] != 1 {
		t.Fatalf("%v %v", seqs, err)
	}
	CloseStore(b)
	if err := os.Remove(filepath.Join(dir, "index.sqlite")); err != nil {
		t.Fatal(err)
	}
	if err := b.Reindex(); err != nil {
		t.Fatal(err)
	}
	if n, err := b.Count(); err != nil || n != 1 {
		t.Fatalf("count %d %v", n, err)
	}
}

func TestBlobSpillAndUncovered(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	b.SetBlobThreshold(32)
	full := strings.Repeat("x", 600) + " BLOB-TAIL"
	if err := b.Append(Entry{Message: provider.Message{Role: provider.RoleTool, Content: provider.ToolResult("c1", full)}}); err != nil {
		t.Fatal(err)
	}
	if err := b.Append(Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("kept raw")}}); err != nil {
		t.Fatal(err)
	}
	got, err := b.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got[0].Message.Text(), "BLOB-TAIL") || got[0].Blob == "" {
		t.Fatalf("preview %+v", got[0])
	}
	hydrated, err := b.Hydrate(got[0])
	if err != nil || hydrated.Message.Text() != full {
		t.Fatalf("%v %q", err, hydrated.Message.Text())
	}
	seqs, err := b.Search("BLOB-TAIL", 2)
	if err != nil || len(seqs) != 1 || seqs[0] != 1 {
		t.Fatalf("fts %v %v query %q", seqs, err, ftsQuery("BLOB-TAIL"))
	}
	if err := b.SaveState(State{Blocks: []SummaryBlock{{From: 1, To: 1, Text: "sum"}}}); err != nil {
		t.Fatal(err)
	}
	tail, err := b.Uncovered([]SummaryBlock{{From: 1, To: 1, Text: "sum"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 1 || tail[0].Message.Text() != "kept raw" {
		t.Fatalf("uncovered %+v", tail)
	}
}
