package skill_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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
	prefix := kernel.StablePrefix("Be brief.", "rules", "", []string{"tools"})
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
	lib := skill.Dir(root)
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
	slug, err := skill.Import(root, srcLink)
	if err != nil || slug != "ship" {
		t.Fatalf("import %s %v", slug, err)
	}
	dest := filepath.Join(skill.Dir(root), "ship", "SKILL.md")
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
	one := skill.FormatPrompt("", []string{"ship: Pack a release"})
	two := skill.FormatPrompt("", []string{"ship: Pack a release", "watch: Watch files"})
	if !strings.Contains(one, skill.Precedence) || !strings.Contains(one, skill.ReadWith) || !strings.Contains(one, "- ship: Pack a release") {
		t.Fatal(one)
	}
	if strings.Count(two, "\n") != strings.Count(one, "\n")+1 {
		t.Fatalf("lines\n%s\n%s", one, two)
	}
	if strings.Contains(two, "SECRET") || strings.Contains(two, "inputSchema") || strings.Contains(one, "Directory:") {
		t.Fatal(two)
	}
	const winDir = `C:\Users\ada\.xbot-dev\skills`
	if withDir := skill.FormatPrompt(winDir, []string{"ship: Pack a release"}); !strings.Contains(withDir, "Directory: "+winDir+"\n") {
		t.Fatal(withDir)
	}
	const marker = "SUMMARY-SHOULD-DROP"
	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, "skill: "+marker+" "+strings.Repeat("字", 30))
	}
	text := skill.FormatPrompt("", lines)
	if strings.Contains(text, marker) {
		t.Fatal("descriptions kept over cap")
	}
	if strings.Count(text, "\n") < 39 || !strings.Contains(text, "- skill") || !strings.Contains(text, skill.Precedence) || !strings.Contains(text, skill.ReadWith) {
		t.Fatal(text)
	}
}

func TestImportDataOverwriteNeedsConsent(t *testing.T) {
	root := t.TempDir()
	v1 := []byte("---\nname: 写周报\ndescription: first\n---\n\nONE\n")
	v2 := []byte("---\nname: 写周报\ndescription: second\n---\n\nTWO\n")
	got, err := skill.ImportData(root, "weekly", v1, false)
	if err != nil || got.Slug != "weekly" || got.Name != "写周报" {
		t.Fatalf("first %+v %v", got, err)
	}
	got, err = skill.ImportData(root, "weekly", v2, false)
	if !errors.Is(err, skill.ErrExists) || got.Name != "写周报" {
		t.Fatalf("same name must ask first: %+v %v", got, err)
	}
	raw, _ := os.ReadFile(filepath.Join(skill.Dir(root), "weekly", "SKILL.md"))
	if !strings.Contains(string(raw), "ONE") {
		t.Fatalf("refused import changed the library: %s", raw)
	}
	if _, err := skill.ImportData(root, "weekly", v2, true); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(filepath.Join(skill.Dir(root), "weekly", "SKILL.md"))
	if !strings.Contains(string(raw), "TWO") {
		t.Fatalf("overwrite did not replace: %s", raw)
	}
}

func TestImportDataRejectsBadInput(t *testing.T) {
	root := t.TempDir()
	good := []byte("---\nname: ship\ndescription: Pack\n---\n\nbody\n")
	if _, err := skill.ImportData(root, "weekly", []byte("no front matter\n"), false); !errors.Is(err, skill.ErrNoMeta) {
		t.Fatalf("no meta: %v", err)
	}
	// A folder name that is not a plain slug never reaches the filesystem; the frontmatter name is tried instead.
	for _, dir := range []string{"../evil", "a/b", ".hidden", "UPPER", ""} {
		got, err := skill.ImportData(root, dir, good, true)
		if err != nil || got.Slug != "ship" {
			t.Fatalf("dir %q: %+v %v", dir, got, err)
		}
	}
	bad := []byte("---\nname: ../../escape\ndescription: x\n---\n")
	if _, err := skill.ImportData(root, "../x", bad, true); !errors.Is(err, skill.ErrBadSlug) {
		t.Fatalf("bad slug: %v", err)
	}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if e.Name() != "skills" {
			t.Fatalf("wrote outside the library: %s", e.Name())
		}
	}
}

func TestImportDataSameNameRace(t *testing.T) {
	root := t.TempDir()
	raw := []byte("---\nname: ship\ndescription: Pack\n---\n\nbody\n")
	const n = 16
	var wg sync.WaitGroup
	var made, asked atomic.Int32
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch _, err := skill.ImportData(root, "ship", raw, false); {
			case err == nil:
				made.Add(1)
			case errors.Is(err, skill.ErrExists):
				asked.Add(1)
			default:
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if made.Load() != 1 || asked.Load() != n-1 {
		t.Fatalf("made %d asked %d", made.Load(), asked.Load())
	}
}
