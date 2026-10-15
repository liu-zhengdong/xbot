package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"xbot/internal/person"
)

func cmdPerson(root string, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: xbot person unlink [--force] <link:id>")
	}
	switch args[0] {
	case "unlink":
		return cmdPersonUnlink(root, args[1:])
	default:
		return fmt.Errorf("unknown person command %q", args[0])
	}
}

func cmdPersonUnlink(root string, args []string) error {
	ordered, err := reorderFlags(args, map[string]bool{})
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("person unlink", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	force := fs.Bool("force", false, "unlink the last local account")
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: xbot person unlink [--force] <link:id>")
	}
	acct, err := parseAccount(fs.Arg(0))
	if err != nil {
		return err
	}
	people, err := person.List(root)
	if err != nil {
		return err
	}
	for _, p := range people {
		for _, a := range p.Accounts {
			if a != acct {
				continue
			}
			if lastLocalAccount(p, acct) {
				fmt.Fprintf(os.Stderr, "warning: %s is the last local account\n", fs.Arg(0))
				if !*force {
					return errors.New("refusing to unlink the last local account; pass --force")
				}
			}
			if err := person.Unlink(root, p.ID, acct); err != nil {
				return err
			}
			fmt.Printf("unlinked %s from %s\n", fs.Arg(0), p.ID)
			return nil
		}
	}
	return errors.New("person: account not linked")
}

func lastLocalAccount(p person.Person, acct person.Account) bool {
	if !localAccount(acct) {
		return false
	}
	for _, a := range p.Accounts {
		if a != acct && localAccount(a) {
			return false
		}
	}
	return true
}

func localAccount(a person.Account) bool {
	return a.Link == "terminal" && strings.HasPrefix(a.ExternalID, "uid:")
}

func parseAccount(s string) (person.Account, error) {
	s = strings.TrimSpace(s)
	i := strings.Index(s, ":")
	if i <= 0 || i >= len(s)-1 {
		return person.Account{}, errors.New("usage: xbot person unlink [--force] <link:id>")
	}
	return person.Account{Link: s[:i], ExternalID: s[i+1:]}, nil
}
