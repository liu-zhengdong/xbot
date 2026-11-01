package bot

import (
	"os"
	"path/filepath"
	"testing"
	"xbot/internal/work"
)

func TestProfileChangeColdCwdUsesPhysicalRootIdentity(t *testing.T) {
	root := t.TempDir()
	b, err := Create(root, "physical-before", "duties", nil)
	if err != nil {
		t.Fatal(err)
	}
	physical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "root-alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	aliased, err := Load(alias, b.Name())
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(work.BotDir(physical, b.Name()), "project")
	if err := os.MkdirAll(project, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(Dir(root, b.Name()), "cwd"), []byte(project+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	next := "physical-after"
	_, result, err := aliased.ApplyProfileChange(normalizedChange(t, ProfileChange{Name: &next}))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Moved {
		t.Fatal(result)
	}
	fresh, err := Load(alias, next)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(Dir(alias, fresh.Name()), "cwd"))
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(work.BotDir(physical, next), "project") + "\n"
	if string(raw) != expected {
		t.Fatalf("cold physical cwd %q want %q", raw, expected)
	}
}
