package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/link"
)

func cmdLink(root string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "list":
		if len(args) > 2 {
			return errUsage
		}
		var b *bot.Bot
		if len(args) == 2 {
			var err error
			b, err = bot.Load(root, args[1])
			if err != nil {
				return err
			}
		}
		granted := map[string]bool{}
		if b != nil {
			for _, name := range b.Config().Links {
				granted[name] = true
			}
		}
		for _, e := range link.Catalog() {
			state := "listed"
			if e.Available {
				state = "available"
			}
			if b != nil && e.Grant != "" && granted[e.Grant] {
				state = "installed"
			}
			fmt.Printf("%s\t%s\t%s\t%s\n", e.ID, e.Kind, state, strings.Join(e.Capabilities, ","))
		}
		return nil
	case "install":
		if len(args) != 3 {
			return errUsage
		}
		b, err := bot.Load(root, args[1])
		if err != nil {
			return err
		}
		e, ok := link.Lookup(args[2])
		if !ok {
			return link.ErrUnknown
		}
		if !e.Available || e.Grant == "" {
			return link.ErrUnavailable
		}
		if len(e.Secrets) > 0 {
			vals := map[string]string{}
			for _, name := range e.Secrets {
				v := strings.TrimSpace(os.Getenv(name))
				if v == "" {
					return fmt.Errorf("link: %s is empty", name)
				}
				vals[name] = v
			}
			if err := link.WriteSecrets(b.SecretPath(), vals); err != nil {
				return err
			}
		}
		if err := b.GrantLink(e.Grant); err != nil {
			if len(e.Secrets) > 0 {
				_ = link.DropSecrets(b.SecretPath(), e.Secrets)
			}
			return err
		}
		fmt.Printf("installed %s on %s\n", args[2], b.Name())
		return nil
	case "remove":
		if len(args) != 3 {
			return errUsage
		}
		b, err := bot.Load(root, args[1])
		if err != nil {
			return err
		}
		e, ok := link.Lookup(args[2])
		if !ok {
			return link.ErrUnknown
		}
		if e.Grant != "" {
			if err := b.RevokeLink(e.Grant); err != nil {
				return err
			}
		}
		if len(e.Secrets) > 0 {
			if err := link.DropSecrets(b.SecretPath(), e.Secrets); err != nil {
				return err
			}
		}
		fmt.Printf("removed %s from %s\n", args[2], b.Name())
		return nil
	default:
		return errors.New("usage: xbot link list|install|remove")
	}
}
