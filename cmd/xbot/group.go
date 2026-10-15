package main

import (
	"fmt"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/person"
)

func cmdGroup(root string, args []string) error {
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
		view, err := bot.Folders(root, owner)
		if err != nil {
			return err
		}
		for _, f := range view.Folders {
			fmt.Printf("%s\t%d\t%s\n", f.Name, f.Count, strings.Join(f.Bots, ","))
		}
		return nil
	case "create":
		if len(args) != 2 {
			return errUsage
		}
		if err := bot.CreateFolder(root, owner, args[1]); err != nil {
			return err
		}
		fmt.Printf("created group %s\n", strings.TrimSpace(args[1]))
		return nil
	case "rename":
		if len(args) != 3 {
			return errUsage
		}
		if err := bot.RenameFolder(root, owner, args[1], args[2]); err != nil {
			return err
		}
		fmt.Printf("renamed group %s\n", strings.TrimSpace(args[2]))
		return nil
	case "delete":
		if len(args) != 2 {
			return errUsage
		}
		if err := bot.DeleteFolder(root, owner, args[1]); err != nil {
			return err
		}
		fmt.Printf("deleted group %s\n", strings.TrimSpace(args[1]))
		return nil
	case "set":
		if len(args) == 3 && args[2] == "--clear" {
			if err := bot.SetBotFolder(root, owner, args[1], ""); err != nil {
				return err
			}
			fmt.Printf("%s group cleared\n", args[1])
			return nil
		}
		if len(args) != 3 {
			return errUsage
		}
		if err := bot.SetBotFolder(root, owner, args[1], args[2]); err != nil {
			return err
		}
		fmt.Printf("%s group %s\n", args[1], strings.TrimSpace(args[2]))
		return nil
	default:
		return errUsage
	}
}

func terminalOwner(root string) (string, error) {
	ext, display, err := link.Account()
	if err != nil {
		return "", err
	}
	who, err := person.Identify(root, person.Account{Link: link.NameTerminal, ExternalID: ext}, display)
	if err != nil {
		return "", err
	}
	return who.ID, nil
}
