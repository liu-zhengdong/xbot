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
