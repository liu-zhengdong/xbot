package bot

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"xbot/internal/provider"
)

func TestFormatQuoteCutsAt300Runes(t *testing.T) {
	body := strings.Repeat("字", 300) + "TAILMARKER"
	got := FormatQuote(4, body)
	if !strings.HasPrefix(got, "user is replying to your earlier message (seq 4): ") {
		t.Fatalf("prefix %q", got)
	}
	if strings.Contains(got, "TAILMARKER") {
		t.Fatalf("full text leaked: %s", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("missing ellipsis: %s", got[len(got)-8:])
	}
	quoted := strings.TrimPrefix(got, "user is replying to your earlier message (seq 4): ")
	quoted = strings.TrimSuffix(quoted, "…")
	if utf8.RuneCountInString(quoted) != QuoteRunes {
		t.Fatalf("runes %d", utf8.RuneCountInString(quoted))
	}
	short := FormatQuote(1, "hello")
	if short != "user is replying to your earlier message (seq 1): hello" {
		t.Fatalf("short %q", short)
	}
}

func TestTakeReplyLine(t *testing.T) {
	seq, rest, ok := TakeReplyLine("reply_to: 3\nhello")
	if !ok || seq != 3 || rest != "hello" {
		t.Fatalf("got %d %q %v", seq, rest, ok)
	}
	if _, _, ok := TakeReplyLine("hello"); ok {
		t.Fatal("plain text looked like a reply line")
	}
	if _, _, ok := TakeReplyLine("reply_to: 0\nno"); ok {
		t.Fatal("zero seq accepted")
	}
	if _, _, ok := TakeReplyLine("reply_to:\nno"); ok {
		t.Fatal("empty seq accepted")
	}
	_, rest, ok = TakeReplyLine("reply_to: 3\n hello")
	if !ok || rest != "hello" {
		t.Fatalf("leading space kept %q", rest)
	}
}

func TestExportIncludesReplyTo(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "ada", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	rows := []Entry{
		{Time: when, Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("earlier")}},
		{Time: when.Add(time.Minute), ReplyTo: 1, Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("and this")}},
		{Time: when.Add(2 * time.Minute), ReplyTo: 2, Message: provider.Message{Role: provider.RoleAssistant, Content: provider.TextContent("back")}},
	}
	for _, e := range rows {
		if err := b.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	md, err := ExportTranscript(b, TranscriptExport{Format: "md"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "reply_to: 1") || !strings.Contains(md, "reply_to: 2") || !strings.Contains(md, "and this") {
		t.Fatalf("markdown\n%s", md)
	}
	raw, err := ExportTranscript(b, TranscriptExport{Format: "jsonl"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"reply_to":1`) || !strings.Contains(raw, `"reply_to":2`) {
		t.Fatalf("jsonl\n%s", raw)
	}
	if strings.Contains(raw, `"reply_to":0`) {
		t.Fatalf("zero reply leaked\n%s", raw)
	}
}
