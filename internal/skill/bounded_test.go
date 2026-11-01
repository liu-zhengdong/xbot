package skill_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/skill"
)

func TestBoundedSkillOpenAndCandidates(t *testing.T) {
	root := t.TempDir()
	body := strings.Repeat("instruction\n", 2000) + "TAIL_SENTINEL"
	if err := skill.Write(root, "ship", "publish", body, "发布"); err != nil {
		t.Fatal(err)
	}
	info, got, err := skill.OpenBounded(skill.Dir(root), "ship", 1<<20)
	if err != nil || got != body || info.Name != "ship" || info.Title != "发布" {
		t.Fatalf("full parse: info=%+v bytes=%d err=%v", info, len(got), err)
	}
	if err := skill.Write(root, "empty", "empty instructions", ""); err != nil {
		t.Fatal(err)
	}
	if _, got, err := skill.OpenBounded(skill.Dir(root), "empty", 1024); err != nil || got != "" {
		t.Fatalf("empty parsed body=%q err=%v", got, err)
	}
	linked := filepath.Join(skill.Dir(root), "linked")
	if err := os.Symlink(filepath.Join(skill.Dir(root), "ship"), linked); err != nil {
		t.Fatal(err)
	}
	infos, err := skill.ListBounded(skill.Dir(root), 1<<20)
	if err != nil || len(infos) != 3 || infos[0].Slug != "empty" || infos[1].Slug != "linked" || infos[2].Slug != "ship" {
		t.Fatalf("linked sorted library: %+v err=%v", infos, err)
	}
	if err := os.WriteFile(filepath.Join(skill.Dir(root), "empty", "SKILL.md"), []byte(strings.Repeat("x", 256)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := skill.OpenBounded(skill.Dir(root), "empty", 256); err != nil {
		t.Fatalf("exact raw bound: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skill.Dir(root), "empty", "SKILL.md"), []byte(strings.Repeat("x", 257)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := skill.OpenBounded(skill.Dir(root), "empty", 256); err == nil {
		t.Fatal("max+1 raw bytes accepted")
	}
	if _, err := skill.ListBounded(skill.Dir(root), 256); err == nil {
		t.Fatal("oversized candidate accepted during enumeration")
	}
}
