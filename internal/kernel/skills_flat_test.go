package kernel

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/skill"
)

// This test uses raw flat fixtures so it also compiles against the pre-change API.
func TestFlatSkillsAllLocalBots(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "skills", "foo", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: foo\ndescription: test\n---\nbody"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		owners []string
	}{{"single", []string{"u1"}}, {"other", []string{"u2"}}, {"multi", []string{"u1", "u2"}}, {"orphan", nil}} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := bot.Create(root, tc.name, "persona", []string{link.NameSkills})
			if err != nil {
				t.Fatal(err)
			}
			if err := b.SetOwners(tc.owners); err != nil {
				t.Fatal(err)
			}
			k := skillKernel(b)
			infos, err := k.skillInfos()
			if err != nil || len(infos) != 1 || infos[0].Slug != "foo" {
				t.Fatalf("flat library missing: %+v %v", infos, err)
			}
			if _, _, err := k.openSkill("foo"); err != nil {
				t.Fatal(err)
			}
			if _, err := (writeSkillTool{k: k}).Run(context.Background(), []byte(`{"slug":"foo","description":"test","body":"body","scope":"self"}`)); err != nil {
				t.Fatal(err)
			}
			if err := b.EnableSkill("foo"); err != nil {
				t.Fatal(err)
			}
			// Reset switches changed by scope=self for the next subtest.
			cfgs, err := bot.List(root)
			if err != nil {
				t.Fatal(err)
			}
			for _, cfg := range cfgs {
				other, err := bot.Load(root, cfg.Name)
				if err != nil {
					t.Fatal(err)
				}
				if err := other.EnableSkill("foo"); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
	otherRoot := t.TempDir()
	other, err := bot.Create(otherRoot, "elsewhere", "persona", []string{link.NameSkills})
	if err != nil {
		t.Fatal(err)
	}
	if infos, err := skillKernel(other).skillInfos(); err != nil || len(infos) != 0 {
		t.Fatalf("cross-root leak: %+v %v", infos, err)
	}
	if infos, err := skill.List(skill.Dir(root)); err != nil || len(infos) != 1 {
		t.Fatalf("unexpected library: %+v %v", infos, err)
	}
}
