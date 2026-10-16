package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/vault"
)

// writeLinkSecrets stores install-time values: connector tokens in the vault,
// anything else in the legacy plaintext file.
func writeLinkSecrets(b *bot.Bot, store vault.Backend, values map[string]string) error {
	rest := map[string]string{}
	for name, value := range values {
		kind := link.KindForSecret(name)
		if kind == "" || store == nil {
			rest[name] = value
			continue
		}
		if err := link.SetConnectorSecret(store, kind, value); err != nil {
			return err
		}
	}
	if len(rest) == 0 {
		return nil
	}
	return link.WriteSecrets(b.SecretPath(), rest)
}

// dropLinkSecrets removes install-time values from the vault and the file.
func dropLinkSecrets(b *bot.Bot, store vault.Backend, e link.CatalogEntry) error {
	for _, name := range e.Secrets {
		kind := link.KindForSecret(name)
		if kind == "" || store == nil {
			continue
		}
		if err := link.RemoveConnectorSecret(store, kind); err != nil {
			return err
		}
	}
	return link.DropSecrets(b.SecretPath(), e.Secrets)
}

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
		var store vault.Backend
		if len(e.Secrets) > 0 {
			vals := map[string]string{}
			for _, name := range e.Secrets {
				v := strings.TrimSpace(os.Getenv(name))
				if v == "" {
					return fmt.Errorf("link: %s is empty", name)
				}
				vals[name] = v
			}
			opened, err := vault.OpenBackend(b.Name(), b.VaultDir(), b.VaultKeyPath())
			if err != nil {
				return err
			}
			store = opened
			if err := writeLinkSecrets(b, store, vals); err != nil {
				return err
			}
		}
		if err := b.GrantLink(e.Grant); err != nil {
			if len(e.Secrets) > 0 {
				_ = dropLinkSecrets(b, store, e)
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
			store, err := vault.OpenBackend(b.Name(), b.VaultDir(), b.VaultKeyPath())
			if err != nil {
				return err
			}
			if err := dropLinkSecrets(b, store, e); err != nil {
				return err
			}
		}
		fmt.Printf("removed %s from %s\n", args[2], b.Name())
		return nil
	default:
		return errors.New("usage: xbot link list|install|remove")
	}
}
