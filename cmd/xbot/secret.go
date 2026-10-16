package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/vault"
)

func cmdSecret(root string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "set":
		return cmdSecretSet(root, args[1:])
	case "list":
		return cmdSecretList(root, args[1:])
	case "rm":
		return cmdSecretRm(root, args[1:])
	case "migrate":
		return cmdSecretMigrate(root, args[1:])
	default:
		return errors.New("usage: xbot secret set|list|rm|migrate")
	}
}

// openSecretBackend opens the per-bot credential backend: the daemon client
// when XBOT_VAULT_SOCKET is set, otherwise the local same-user Store.
func openSecretBackend(b *bot.Bot) (vault.Backend, error) {
	return vault.OpenBackend(b.Name(), b.VaultDir(), b.VaultKeyPath())
}

// cmdSecretSet reads the value from stdin so it never appears in argv or the
// process list. Only the name and its domains are printed.
func cmdSecretSet(root string, args []string) error {
	ordered, err := reorderFlags(args, map[string]bool{"domains": true})
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("secret set", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	domains := fs.String("domains", "", "comma-separated allowed domains")
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return errors.New("usage: xbot secret set <bot> NAME --domains a.com,b.com")
	}
	b, err := bot.Load(root, fs.Arg(0))
	if err != nil {
		return err
	}
	name := strings.TrimSpace(fs.Arg(1))
	if name == "" {
		return errors.New("secret: empty name")
	}
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil {
		return err
	}
	value := strings.TrimRight(string(raw), "\r\n")
	if value == "" {
		return errors.New("secret: empty value on stdin")
	}
	store, err := openSecretBackend(b)
	if err != nil {
		return err
	}
	if err := store.Set(name, value, strings.Split(*domains, ",")); err != nil {
		return err
	}
	list := store.List()
	for _, sec := range list {
		if sec.Name == name {
			fmt.Printf("stored %s for %s -> %s\n", name, b.Name(), strings.Join(sec.Domains, ","))
			return nil
		}
	}
	fmt.Printf("stored %s for %s\n", name, b.Name())
	return nil
}

func cmdSecretList(root string, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: xbot secret list <bot>")
	}
	b, err := bot.Load(root, args[0])
	if err != nil {
		return err
	}
	store, err := openSecretBackend(b)
	if err != nil {
		return err
	}
	for _, sec := range store.List() {
		fmt.Printf("%s\t%s\n", sec.Name, strings.Join(sec.Domains, ","))
	}
	return nil
}

func cmdSecretRm(root string, args []string) error {
	if len(args) != 2 {
		return errors.New("usage: xbot secret rm <bot> NAME")
	}
	b, err := bot.Load(root, args[0])
	if err != nil {
		return err
	}
	store, err := openSecretBackend(b)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(args[1])
	if err := store.Remove(name); err != nil {
		return err
	}
	fmt.Printf("removed %s from %s\n", name, b.Name())
	return nil
}

// cmdSecretMigrate copies every secret from the bot's local vault into the
// daemon and, only after every set succeeded, removes the local vault dir and
// key file. Without XBOT_VAULT_SOCKET there is nothing to migrate to.
func cmdSecretMigrate(root string, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: xbot secret migrate <bot>")
	}
	socket := strings.TrimSpace(os.Getenv("XBOT_VAULT_SOCKET"))
	if socket == "" {
		return errors.New("secret migrate: XBOT_VAULT_SOCKET is not set")
	}
	b, err := bot.Load(root, args[0])
	if err != nil {
		return err
	}
	local, err := vault.Open(b.VaultDir(), b.VaultKeyPath())
	if err != nil {
		return err
	}
	remote, err := vault.OpenClient(socket, b.Name())
	if err != nil {
		return err
	}
	localList := local.List()
	for _, sec := range localList {
		value, ok := local.Lookup(sec.Name)
		if !ok {
			return fmt.Errorf("secret migrate: %s disappeared during migration", sec.Name)
		}
		if err := remote.Set(sec.Name, value.Value, sec.Domains); err != nil {
			return fmt.Errorf("secret migrate: %s: %w", sec.Name, err)
		}
	}
	// Every set succeeded; now remove the local copy. The key file is only
	// removed after the vault directory is gone, so a partial failure leaves a
	// working local vault.
	if err := os.RemoveAll(b.VaultDir()); err != nil {
		return err
	}
	if err := os.Remove(b.VaultKeyPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_ = os.Remove(filepath.Dir(b.VaultKeyPath()))
	fmt.Printf("migrated %d secrets for %s\n", len(localList), b.Name())
	return nil
}
