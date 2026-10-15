package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/link"
)

func cmdMCP(root string, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "list":
		return cmdMCPList(root, args[1:])
	case "add":
		return cmdMCPAdd(root, args[1:])
	case "remove":
		return cmdMCPRemove(root, args[1:])
	default:
		return errors.New("usage: xbot mcp list|add|remove")
	}
}

func cmdMCPList(root string, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: xbot mcp list <bot>")
	}
	b, err := bot.Load(root, args[0])
	if err != nil {
		return err
	}
	list, err := link.LoadMCP(b.MCPPath())
	if err != nil {
		return err
	}
	for _, s := range list {
		fmt.Printf("%s\t%s\t%s\t%s\n", s.Name, s.Command, strings.Join(s.Args, " "), strings.Join(s.SecretEnvs, ","))
	}
	return nil
}

func cmdMCPAdd(root string, args []string) error {
	ordered, err := reorderFlags(args, map[string]bool{
		"command":    true,
		"arg":        true,
		"secret-env": true,
	})
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("mcp add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	command := fs.String("command", "", "executable")
	var cmdArgs []string
	var secrets []string
	fs.Func("arg", "repeatable argument", func(s string) error {
		cmdArgs = append(cmdArgs, s)
		return nil
	})
	fs.Func("secret-env", "environment variable name", func(s string) error {
		secrets = append(secrets, s)
		return nil
	})
	if err := fs.Parse(ordered); err != nil {
		return err
	}
	if fs.NArg() != 2 || *command == "" {
		return errors.New("usage: xbot mcp add <bot> <name> --command PATH [--arg A]... [--secret-env NAME]...")
	}
	b, err := bot.Load(root, fs.Arg(0))
	if err != nil {
		return err
	}
	s := link.MCPServer{Name: fs.Arg(1), Command: *command, Args: cmdArgs, SecretEnvs: secrets}
	if err := link.AddMCP(b.MCPPath(), s); err != nil {
		return err
	}
	fmt.Printf("added %s on %s\n", s.Name, b.Name())
	return nil
}

func cmdMCPRemove(root string, args []string) error {
	if len(args) != 2 {
		return errors.New("usage: xbot mcp remove <bot> <name>")
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
