package main

import (
	"fmt"
	"os"
	"path/filepath"

	"xbot/internal/bot"
	"xbot/internal/skill"
)

func cmdSkill(root string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	owner, err := terminalOwner(root)
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return errUsage
		}
		return cmdSkillList(root, owner)
	case "import":
		if len(args) != 2 {
			return errUsage
		}
		slug, err := skill.Import(root, owner, args[1])
		if err != nil {
			return err
		}
		fmt.Printf("imported %s\n", slug)
		return nil
	case "show":
		if len(args) != 2 {
			return errUsage
		}
		return cmdSkillShow(root, owner, args[1])
	case "disable":
		if len(args) != 3 {
			return errUsage
		}
		return cmdSkillSet(root, owner, args[1], args[2], true)
	case "enable":
		if len(args) != 3 {
			return errUsage
		}
		return cmdSkillSet(root, owner, args[1], args[2], false)
	default:
		return errUsage
	}
}

func cmdSkillList(root, owner string) error {
	infos, err := skill.List(skill.OwnerDir(root, owner))
	if err != nil {
		return err
	}
	for _, info := range infos {
		fmt.Printf("%s\t%s\t%s\n", info.Slug, info.Name, info.Description)
	}
	return nil
}

func cmdSkillShow(root, owner, slug string) error {
	path := filepath.Join(skill.OwnerDir(root, owner), slug, "SKILL.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("skill: %s not found", slug)
		}
		return err
	}
	fmt.Print(string(raw))
	return nil
}

func cmdSkillSet(root, owner, name, slug string, disable bool) error {
	if !skill.ValidSlug(slug) {
		return fmt.Errorf("skill: bad slug %q", slug)
	}
	b, err := bot.Load(root, name)
	if err != nil {
		return err
	}
	if !botOwned(b, owner) {
		return bot.ErrNotFound
	}
	if _, _, err := skill.Open(skill.OwnerDir(root, owner), slug); err != nil {
		return err
	}
	if disable {
		if err := b.DisableSkill(slug); err != nil {
			return err
		}
		fmt.Printf("disabled %s on %s\n", slug, b.Name())
		return nil
	}
	if err := b.EnableSkill(slug); err != nil {
		return err
	}
	fmt.Printf("enabled %s on %s\n", slug, b.Name())
	return nil
}

func botOwned(b *bot.Bot, owner string) bool {
	for _, id := range b.Config().Owners {
		if id == owner {
			return true
		}
	}
	return false
}
