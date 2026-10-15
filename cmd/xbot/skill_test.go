package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/person"
	"xbot/internal/skill"
)

func TestSkillCLIImportDisable(t *testing.T) {
	root := t.TempDir()
	ext, display, err := link.Account()
	if err != nil {
		t.Fatal(err)
	}
	who, err := person.Identify(root, person.Account{Link: link.NameTerminal, ExternalID: ext}, display)
	if err != nil {
		t.Fatal(err)
	}
	ada, err := bot.Create(root, "ada", "persona", []string{link.NameSkills})
	if err != nil {
		t.Fatal(err)
	}
	if err := ada.SetOwners([]string{who.ID}); err != nil {
		t.Fatal(err)
	}
	bob, err := bot.Create(root, "bob", "persona", []string{link.NameSkills})
	if err != nil {
		t.Fatal(err)
	}
	if err := bob.SetOwners([]string{who.ID}); err != nil {
		t.Fatal(err)
	}
	eve, err := bot.Create(root, "eve", "persona", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := eve.SetOwners([]string{"someone-else"}); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(t.TempDir(), "real")
	if err := os.MkdirAll(real, 0700); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: ship\ndescription: Pack a release\n---\n\nSECRET_BODY\n"
	if err := os.WriteFile(filepath.Join(real, "SKILL.md"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(t.TempDir(), "ship")
	if err := os.Symlink(real, linkPath); err != nil {
		t.Fatal(err)
	}
	if err := cmdSkill(root, []string{"import", linkPath}); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(skill.OwnerDir(root, who.ID), "ship", "SKILL.md")
	fi, err := os.Lstat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("import kept the symlink")
	}
	if err := cmdSkill(root, []string{"list"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdSkill(root, []string{"show", "ship"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdSkill(root, []string{"disable", "ada", "ship"}); err != nil {
		t.Fatal(err)
	}
	ada, err = bot.Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ada.Config().DisabledSkills, ",") != "ship" {
		t.Fatalf("disabled %#v", ada.Config().DisabledSkills)
	}
	bob, err = bot.Load(root, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if len(bob.Config().DisabledSkills) != 0 {
		t.Fatal("disable leaked to another bot")
	}
	if err := cmdSkill(root, []string{"disable", "eve", "ship"}); err == nil {
		t.Fatal("other owner disabled a skill")
	}
	if err := cmdSkill(root, []string{"enable", "ada", "ship"}); err != nil {
		t.Fatal(err)
	}
	ada, err = bot.Load(root, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if len(ada.Config().DisabledSkills) != 0 {
		t.Fatalf("still disabled %#v", ada.Config().DisabledSkills)
	}
}
