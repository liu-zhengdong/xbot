package skill_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/kernel"
	"xbot/internal/skill"
)

func TestDescriptionInPrefixBodyOnDemand(t *testing.T) {
	root := t.TempDir()
	dir := skill.Dir(root)
	if err := os.MkdirAll(filepath.Join(dir, "ship"), 0700); err != nil {
		t.Fatal(err)
	}
	body := "description: Pack a release\n\nSECRET_BODY do not put this in the prefix\n"
	if err := os.WriteFile(filepath.Join(dir, "ship", "SKILL.md"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	lines, err := skill.Lines(dir)
	if err != nil || len(lines) != 1 || lines[0] != "ship: Pack a release" {
		t.Fatalf("%v %v", lines, err)
	}
	prefix := kernel.StablePrefix("Be brief.", "rules", []string{"tools"})
	with := prefix
	if len(lines) > 0 {
		with = prefix + "\n\n## Skills\n- " + lines[0]
	}
	if strings.Contains(with, "SECRET_BODY") {
		t.Fatal("skill body leaked into the prefix")
	}
	if !strings.Contains(with, "ship: Pack a release") {
		t.Fatal(with)
	}
	got, err := skill.Read(dir, "ship")
	if err != nil || !strings.Contains(got, "SECRET_BODY") {
		t.Fatalf("%q %v", got, err)
	}
	empty, err := skill.Lines(filepath.Join(root, "missing"))
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty %v %v", empty, err)
	}
}

func TestSkillSymlinkAndImport(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(t.TempDir(), "real")
	if err := os.MkdirAll(real, 0700); err != nil {
		t.Fatal(err)
	}
	raw := []byte("---\nname: ship\ndescription: Pack a release\n---\n\nSECRET_BODY\n")
	if err := os.WriteFile(filepath.Join(real, "SKILL.md"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	lib := skill.OwnerDir(root, "u1")
	if err := os.MkdirAll(lib, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(lib, "ship")); err != nil {
		t.Fatal(err)
	}
	lines, err := skill.Lines(lib)
	if err != nil || len(lines) != 1 || lines[0] != "ship: Pack a release" {
		t.Fatalf("lines %v %v", lines, err)
	}
	got, err := skill.Read(lib, "ship")
	if err != nil || !strings.Contains(got, "SECRET_BODY") {
		t.Fatalf("%q %v", got, err)
	}

	srcLink := filepath.Join(t.TempDir(), "ship")
	if err := os.Symlink(real, srcLink); err != nil {
		t.Fatal(err)
	}
	slug, err := skill.Import(root, "u2", srcLink)
	if err != nil || slug != "ship" {
		t.Fatalf("import %s %v", slug, err)
	}
	dest := filepath.Join(skill.OwnerDir(root, "u2"), "ship", "SKILL.md")
	fi, err := os.Lstat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink != 0 || fi.Mode().Perm() != 0600 {
		t.Fatalf("mode %v", fi.Mode())
	}
	copied, err := os.ReadFile(dest)
	if err != nil || !strings.Contains(string(copied), "SECRET_BODY") {
		t.Fatalf("%s %v", copied, err)
	}
}

func TestSkillPromptOneLineAndCap(t *testing.T) {
	one := skill.FormatPrompt([]string{"ship: Pack a release"})
	two := skill.FormatPrompt([]string{"ship: Pack a release", "watch: Watch files"})
	if !strings.Contains(one, skill.Precedence) || !strings.Contains(one, "- ship: Pack a release") {
		t.Fatal(one)
	}
	if strings.Count(two, "\n") != strings.Count(one, "\n")+1 {
		t.Fatalf("lines\n%s\n%s", one, two)
	}
	if strings.Contains(two, "SECRET") || strings.Contains(two, "inputSchema") {
		t.Fatal(two)
	}
	const marker = "SUMMARY-SHOULD-DROP"
	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, "skill: "+marker+" "+strings.Repeat("字", 30))
	}
	text := skill.FormatPrompt(lines)
	if strings.Contains(text, marker) {
		t.Fatal("descriptions kept over cap")
	}
	if strings.Count(text, "\n") < 39 || !strings.Contains(text, "- skill") || !strings.Contains(text, skill.Precedence) {
		t.Fatal(text)
	}
}
