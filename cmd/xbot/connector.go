package main

import (
	"errors"
	"fmt"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/link"
)

func cmdConnector(root string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "list":
		return cmdConnectorList(args[1:])
	case "add":
		return cmdConnectorAdd(root, args[1:])
	case "remove":
		return cmdConnectorRemove(root, args[1:])
	default:
		return errors.New("usage: xbot connector list|add|remove")
	}
}

func cmdConnectorList(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: xbot connector list")
	}
	list, err := link.Connectors()
	if err != nil {
		return err
	}
	for _, c := range list {
		where := c.Command
		if c.URL != "" {
			where = c.URL
		}
		fmt.Printf("%s\t%s\t%s\t%s\t%s\n", c.ID, c.Transport, oneLine(c.Title), where, strings.Join(c.SecretEnvs, ","))
	}
	return nil
}

func cmdConnectorAdd(root string, args []string) error {
	if len(args) != 2 {
		return errors.New("usage: xbot connector add <bot> <id>")
	}
	b, err := bot.Load(root, args[0])
	if err != nil {
		return err
	}
	if err := link.EnableConnector(b.MCPPath(), args[1]); err != nil {
		return err
	}
	fmt.Printf("added %s on %s\n", args[1], b.Name())
	return nil
}

func cmdConnectorRemove(root string, args []string) error {
	if len(args) != 2 {
		return errors.New("usage: xbot connector remove <bot> <id>")
	}
	b, err := bot.Load(root, args[0])
	if err != nil {
		return err
	}
	if err := link.RemoveMCP(b.MCPPath(), args[1]); err != nil {
		return err
	}
	fmt.Printf("removed %s from %s\n", args[1], b.Name())
	return nil
}
