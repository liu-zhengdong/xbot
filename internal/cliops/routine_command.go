package cliops

import (
	"errors"
	"fmt"
	"strings"
)

// RoutineCommand contains argv syntax only. Body IO and decoding belong to cmd;
// runtime identity and routine state validation belong to their own boundaries.
type RoutineCommand struct {
	Action, ID, Args, ArgsFile string
	Self, JSON, Help           bool
}

func ParseRoutineRuntime(argv []string) (RoutineCommand, error) {
	var c RoutineCommand
	if len(argv) == 0 {
		return c, errors.New("usage: xbot routine list|create|update|delete --self; use routine --help")
	}
	if argv[0] == "help" || argv[0] == "--help" || argv[0] == "-h" {
		if len(argv) != 1 {
			return c, errors.New("routine help takes no arguments")
		}
		c.Help = true
		return c, nil
	}
	c.Action = argv[0]
	switch c.Action {
	case "list", "create", "update", "delete":
	default:
		return c, fmt.Errorf("routine: unknown runtime action %q", c.Action)
	}
	seen := map[string]bool{}
	var pos []string
	for i := 1; i < len(argv); i++ {
		word := argv[i]
		if !strings.HasPrefix(word, "-") {
			pos = append(pos, word)
			continue
		}
		flag, value, eq := strings.Cut(word, "=")
		if flag == "-h" {
			flag = "--help"
		}
		if seen[flag] {
			return c, fmt.Errorf("routine: repeated %s", flag)
		}
		seen[flag] = true
		switch flag {
		case "--self", "--json", "--help":
			if eq {
				return c, fmt.Errorf("routine: %s takes no value", flag)
			}
			switch flag {
			case "--self":
				c.Self = true
			case "--json":
				c.JSON = true
			case "--help":
				c.Help = true
			}
		case "--args", "--args-file":
			if c.Action != "create" && c.Action != "update" {
				return c, fmt.Errorf("routine: %s requires create or update", flag)
			}
			if !eq {
				if i+1 == len(argv) {
					return c, fmt.Errorf("routine: %s needs a value", flag)
				}
				i++
				value = argv[i]
			}
			if value == "" {
				return c, fmt.Errorf("routine: %s needs a nonempty value", flag)
			}
			if flag == "--args" {
				c.Args = value
			} else {
				c.ArgsFile = value
			}
		default:
			return c, fmt.Errorf("routine: unknown flag %q", flag)
		}
	}
	if seen["--args"] && seen["--args-file"] {
		return c, errors.New("routine: --args and --args-file are mutually exclusive")
	}
	if c.Help {
		return c, nil
	}
	if !c.Self {
		return c, errors.New("routine: runtime commands require --self from an active XBot shell")
	}
	if c.Action == "update" || c.Action == "delete" {
		if len(pos) != 1 || pos[0] == "" {
			return c, fmt.Errorf("routine %s: expected one routine identifier", c.Action)
		}
		c.ID = pos[0]
	} else if len(pos) != 0 {
		return c, fmt.Errorf("routine %s: takes no positional arguments", c.Action)
	}
	if (c.Action == "create" || c.Action == "update") && !seen["--args"] && !seen["--args-file"] {
		return c, fmt.Errorf("routine %s: exactly one of --args or --args-file is required", c.Action)
	}
	return c, nil
}
