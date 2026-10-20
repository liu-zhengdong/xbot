package main

import (
	"errors"
	"fmt"
	"sort"

	"xbot/internal/machine"
	"xbot/internal/review"
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

// cmdPermissions shows or sets whether the permission table asks the owner
// (ADR 0019). Off, the default, asks only for the links card, the vault, an
// outside event touching memory, and a machine set to ask.
func cmdPermissions(root string, args []string) error {
	switch {
	case len(args) == 0:
	case len(args) == 1 && (args[0] == "on" || args[0] == "off"):
		if err := review.SetConfirmTable(root, args[0] == "on"); err != nil {
			return err
		}
	default:
		return errors.New("usage: xbot permissions [on|off]")
	}
	state := "off"
	if review.ConfirmTable(root) {
		state = "on"
	}
	fmt.Printf("permissions\t%s\n", state)
	return nil
}
