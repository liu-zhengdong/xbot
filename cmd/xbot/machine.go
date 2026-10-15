package main

import (
	"errors"
	"fmt"
	"sort"

	"xbot/internal/machine"
)

func cmdMachine(root string, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: xbot machine list|set")
	}
	switch args[0] {
	case "list":
		return cmdMachineList(root, args[1:])
	case "set":
		return cmdMachineSet(root, args[1:])
	default:
		return fmt.Errorf("unknown machine command %q", args[0])
	}
}

func cmdMachineList(root string, args []string) error {
	if len(args) != 0 {
		return errors.New("usage: xbot machine list")
	}
	stored, err := machine.List(root)
	if err != nil {
		return err
	}
	if _, ok := stored[machine.Local]; !ok {
		stored[machine.Local] = machine.Allow
	}
	names := make([]string, 0, len(stored))
	for name := range stored {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Printf("%s\t%s\n", name, stored[name])
	}
	return nil
}

func cmdMachineSet(root string, args []string) error {
	if len(args) != 2 {
		return errors.New("usage: xbot machine set <name> allow|ask|off")
	}
	if err := machine.Set(root, args[0], args[1]); err != nil {
		return err
	}
	fmt.Printf("%s\t%s\n", args[0], args[1])
	return nil
}
