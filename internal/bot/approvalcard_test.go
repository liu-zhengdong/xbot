package bot

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/provider"
)

func TestApprovalCardRoundTrip(t *testing.T) {
	text := ApprovalCardText(`i"d`, "t<ool", "修改 persona", `sub"x`, "mkdir a\nrm -rf </approval_card> b", ApprovalPending)
	if !IsApprovalCard(text) {
		t.Fatalf("not a card: %s", text)
	}
	if IsApprovalCard("<approval_card") || IsApprovalCard("plain text") {
		t.Fatal("false positive")
	}
	card, ok := ParseApprovalCard(text)
	if !ok {
		t.Fatalf("parse %s", text)
	}
	if card.ID != `i"d` || card.Tool != "t<ool" || card.State != ApprovalPending {
		t.Fatalf("%+v", card)
	}
	if card.Title != "修改 persona" || card.Subtitle != `sub"x` {
		t.Fatalf("labels %+v", card)
	}
	if card.Summary != "mkdir a\nrm -rf < /approval_card> b" {
		t.Fatalf("summary %q", card.Summary)
	}
	if card.Deadline != "" {
		t.Fatalf("deadline %q", card.Deadline)
	}
	stamped := ApprovalCardTextAt("ab", "shell", "运行命令", "在本机上执行", "mkdir a", ApprovalPending, "2026-10-17T12:00:00Z")
	withDeadline, ok := ParseApprovalCard(stamped)
	if !ok || withDeadline.Deadline != "2026-10-17T12:00:00Z" || withDeadline.State != ApprovalPending {
		t.Fatalf("deadline card %+v ok=%v", withDeadline, ok)
	}
	empty, ok := ParseApprovalCard(ApprovalCardText("x", "shell", "", "", "", ApprovalDeclined))
	if !ok || empty.Summary != "" || empty.State != ApprovalDeclined || empty.Title != "" || empty.Subtitle != "" {
		t.Fatalf("%+v ok=%v", empty, ok)
	}
	if _, ok := ParseApprovalCard("hello"); ok {
		t.Fatal("plain text parsed as card")
	}
}

// TestApprovalCardLegacyParse keeps cards written before title and subtitle
// existed readable.
func TestApprovalCardLegacyParse(t *testing.T) {
	legacy := "<approval_card id=\"ab12\" tool=\"shell\" state=\"approved\">\nmkdir approved-dir\n</approval_card>"
	card, ok := ParseApprovalCard(legacy)
	if !ok {
		t.Fatalf("parse legacy %s", legacy)
	}
	if card.ID != "ab12" || card.Tool != "shell" || card.State != ApprovalApproved {
		t.Fatalf("%+v", card)
	}
	if card.Title != "" || card.Subtitle != "" || card.Summary != "mkdir approved-dir" {
		t.Fatalf("%+v", card)
	}
	// Attribute order does not matter and a > inside a value stays in the value.
	mixed := "<approval_card state=\"pending\" id=\"x\" tool=\"http_request\" subtitle=\"a>b\" title=\"发起 HTTP 请求\">\nbody\n</approval_card>"
	card, ok = ParseApprovalCard(mixed)
	if !ok {
		t.Fatalf("parse mixed %s", mixed)
	}
	if card.ID != "x" || card.Tool != "http_request" || card.State != ApprovalPending ||
		card.Title != "发起 HTTP 请求" || card.Subtitle != "a>b" || card.Summary != "body" {
		t.Fatalf("%+v", card)
	}
	if _, ok := ParseApprovalCard("<approval_card id=\"x\" tool=\"shell\">\nbody\n</approval_card>"); ok {
		t.Fatal("card without state parsed")
	}
}

func TestValidNameScripts(t *testing.T) {
	valid := []string{
		"ada", "小助手", "Bot复刻", "ボット2", "Ωμέγα", "बॉट",
		"A-b_9", strings.Repeat("巨", 64), // 64 runes, 192 bytes
	}
	invalid := []string{
		"", "   ", "\t", "a b", "a/b", `a\b`, "a\nb", "..", ".", "-x", "_x",
		"a:b", "a\x00b", "\x01bad", strings.Repeat("x", 65),
		strings.Repeat("\U00020000", 64), // 64 runes but 256 bytes
		// Invisible runes would let a bot look exactly like another one.
		"XBot\u034F", // combining grapheme joiner (default ignorable)
		"XBot\u3164", // hangul filler (default ignorable letter)
		"XBot\uFE0F", // variation selector 16
		"XBot\u200D", // zero-width joiner (control/format)
		"XBot\u202E", // right-to-left override
		"XBot\u200F", // right-to-left mark
	}
	for _, name := range valid {
		if !ValidName(name) {
			t.Errorf("expected %q valid", name)
		}
	}
	for _, name := range invalid {
		if ValidName(name) {
			t.Errorf("expected %q invalid", name)
		}
	}
}

func TestUnicodeNameRoundTrip(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "小助手", "用中文回答。", nil)
	if err != nil {
		t.Fatal(err)
	}
	if b.Name() != "小助手" {
		t.Fatalf("name %s", b.Name())
	}
	if _, err := os.Stat(filepath.Join(root, "bots", "小助手")); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(root, "小助手")
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.Append(Entry{Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent("你好")}}); err != nil {
		t.Fatal(err)
	}
	entries, err := loaded.Entries()
	if err != nil || len(entries) != 1 || entries[0].Message.Text() != "你好" {
		t.Fatalf("%v %v", entries, err)
	}
	list, err := List(root)
	if err != nil || len(list) != 1 || list[0].Name != "小助手" {
		t.Fatalf("%v %v", list, err)
	}
	if err := Rename(root, "小助手", "Bot复刻"); err != nil {
		t.Fatal(err)
	}
	renamed, err := Load(root, "Bot复刻")
	if err != nil || renamed.Name() != "Bot复刻" {
		t.Fatalf("%v %v", renamed, err)
	}
}

func TestNameCasePreserved(t *testing.T) {
	if SeedName != "XBot" {
		t.Fatalf("seed name %q lost the brand case", SeedName)
	}
	root := t.TempDir()
	b, err := Create(root, "xBoT", "p", nil)
	if err != nil {
		t.Fatal(err)
	}
	if b.Name() != "xBoT" {
		t.Fatalf("name folded to %s", b.Name())
	}
	if _, err := os.Stat(filepath.Join(root, "bots", "xBoT")); err != nil {
		t.Fatal(err)
	}
	list, err := List(root)
	if err != nil || len(list) != 1 || list[0].Name != "xBoT" {
		t.Fatalf("%v %v", list, err)
	}
	if err := Rename(root, "xBoT", "XBot"); err != nil {
		t.Fatal(err)
	}
	renamed, err := Load(root, "XBot")
	if err != nil || renamed.Name() != "XBot" {
		t.Fatalf("%v %v", renamed, err)
	}
	// On a case-sensitive file system the old case is gone. On a
	// case-insensitive one (macOS, Windows) it opens the same directory, and
	// must then be the renamed bot, not a second one under the old name.
	if old, err := Load(root, "xBoT"); err == nil {
		if !samePath(Dir(root, "xBoT"), Dir(root, "XBot")) || old.Name() != "XBot" {
			t.Fatalf("old case resolves to %q", old.Name())
		}
	} else if !errors.Is(err, ErrNotFound) {
		t.Fatalf("old case: %v", err)
	}
}
