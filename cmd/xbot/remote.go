package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"xbot/internal/remote"
)

func cmdRemote(root string, args []string) error {
	usage := errors.New("usage: xbot remote id | gateway [URL] | add <name> <key> [--person ID] | list | remove <name>")
	if len(args) == 0 {
		return usage
	}
	switch {
	case args[0] == "id" && len(args) == 1:
		_, id, key, err := remote.Identity(root)
		if err != nil {
			return err
		}
		fmt.Printf("id %s\nkey %s\n", id, key)
	case args[0] == "gateway" && len(args) == 2:
		return remote.SetGateway(root, args[1])
	case (args[0] == "gateway" || args[0] == "list") && len(args) == 1:
		cfg, err := remote.Load(root)
		if err != nil {
			return err
		}
		if args[0] == "gateway" {
			fmt.Println(cfg.Gateway)
		} else {
			for _, d := range cfg.Devices {
				fmt.Printf("%s\t%s\t%s\n", d.Name, d.ID, d.Person)
			}
		}
	case args[0] == "add":
		ordered, err := reorderFlags(args[1:], map[string]bool{"person": true})
		if err != nil {
			return err
		}
		fs := flag.NewFlagSet("remote add", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		person := fs.String("person", "", "local person id")
		if err := fs.Parse(ordered); err != nil {
			return err
		}
		if fs.NArg() != 2 {
			return usage
		}
		_, err = remote.AddDevice(root, fs.Arg(0), fs.Arg(1), *person)
		return err
	case args[0] == "remove" && len(args) == 2:
		return remote.RemoveDevice(root, args[1])
	default:
		return usage
	}
	return nil
}
