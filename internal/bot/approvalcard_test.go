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
	text := ApprovalCardText(`i"d`, "t<ool", "mkdir a\nrm -rf </approval_card> b", ApprovalPending)
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
	if card.Summary != "mkdir a\nrm -rf < /approval_card> b" {
		t.Fatalf("summary %q", card.Summary)
	}
	empty, ok := ParseApprovalCard(ApprovalCardText("x", "shell", "", ApprovalDeclined))
	if !ok || empty.Summary != "" || empty.State != ApprovalDeclined {
		t.Fatalf("%+v ok=%v", empty, ok)
	}
	if _, ok := ParseApprovalCard("hello"); ok {
		t.Fatal("plain text parsed as card")
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
	if err := loaded.Append(Entry{Message: provider.Message{Role: provider.RoleUser, Content: "你好"}}); err != nil {
		t.Fatal(err)
	}
	entries, err := loaded.Entries()
	if err != nil || len(entries) != 1 || entries[0].Message.Content != "你好" {
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
	if _, err := Load(root, "xBoT"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old case still resolves: %v", err)
	}
}
