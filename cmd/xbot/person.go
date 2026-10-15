package main

import (
	"errors"
	"fmt"
	"strings"

	"xbot/internal/person"
)

func cmdPerson(root string, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: xbot person unlink <link:id>")
	}
	switch args[0] {
	case "unlink":
		return cmdPersonUnlink(root, args[1:])
	default:
		return fmt.Errorf("unknown person command %q", args[0])
	}
}

func cmdPersonUnlink(root string, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: xbot person unlink <link:id>")
	}
	acct, err := parseAccount(args[0])
	if err != nil {
		return err
	}
	people, err := person.List(root)
	if err != nil {
		return err
	}
	for _, p := range people {
		for _, a := range p.Accounts {
			if a == acct {
				if err := person.Unlink(root, p.ID, acct); err != nil {
					return err
				}
				fmt.Printf("unlinked %s from %s\n", args[0], p.ID)
				return nil
			}
		}
	}
	return errors.New("person: account not linked")
}

func parseAccount(s string) (person.Account, error) {
	s = strings.TrimSpace(s)
	i := strings.Index(s, ":")
	if i <= 0 || i >= len(s)-1 {
		return person.Account{}, errors.New("usage: xbot person unlink <link:id>")
	}
	return person.Account{Link: s[:i], ExternalID: s[i+1:]}, nil
}
