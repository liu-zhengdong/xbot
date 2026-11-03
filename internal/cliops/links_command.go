package cliops

import (
	"errors"
	"fmt"
	"strings"
)

type LinksCommand struct {
	Action, Query, QueryFile, ID string
	Self, JSON, Help             bool
}

// ParseLinksRuntime consumes syntax only; args excludes links. The noun level
// accepts exactly search, load, install, and remove, each with the same strict
// flag layer as ParseCompactRuntime: no aliases, value flags consume the next
// argument literally, and help stays offline at both levels.
func ParseLinksRuntime(args []string) (LinksCommand, error) {
	var command LinksCommand
	if len(args) == 0 {
		return command, errors.New("links: expected search, load, install, or remove")
	}
	switch args[0] {
	case "search", "load", "install", "remove":
		command.Action, args = args[0], args[1:]
	default:
		if args[0] != "--help" && args[0] != "-h" {
			return command, fmt.Errorf("links: unexpected subcommand %q", args[0])
		}
	}
	seen := map[string]bool{}
	hasID := false
	for i := 0; i < len(args); i++ {
		if command.Action != "" && command.Action != "search" && !strings.HasPrefix(args[i], "-") {
			if hasID {
				return command, fmt.Errorf("links: unexpected argument %q", args[i])
			}
			hasID = true
			command.ID = args[i]
			continue
		}
		flag, value, equals := strings.Cut(args[i], "=")
		if seen[flag] {
			return command, fmt.Errorf("links: repeated %s", flag)
		}
		seen[flag] = true
		switch flag {
		case "--self", "--json", "--help":
			if equals {
				return command, fmt.Errorf("links: %s takes no value", flag)
			}
			switch flag {
			case "--self":
				command.Self = true
			case "--json":
				command.JSON = true
			case "--help":
				command.Help = true
			}
		case "--query", "--query-file":
			if command.Action != "search" {
				return command, fmt.Errorf("links: %s is not valid for %q", flag, command.Action)
			}
			if !equals {
				if i+1 == len(args) {
					return command, fmt.Errorf("links: %s needs a value", flag)
				}
				i++
				value = args[i]
			}
			if flag == "--query" {
				command.Query = value
			} else {
				command.QueryFile = value
			}
		default:
			return command, fmt.Errorf("links: unexpected argument %q", args[i])
		}
	}
	if seen["--query"] && seen["--query-file"] {
		return command, errors.New("links: --query and --query-file are mutually exclusive")
	}
	if seen["--query-file"] && command.QueryFile == "" {
		return command, errors.New("links: --query-file needs a nonempty path")
	}
	if command.Help {
		return command, nil
	}
	if command.Action == "" {
		return command, errors.New("links: expected search, load, install, or remove")
	}
	if !command.Self {
		return command, errors.New("links: requires --self from an active XBot shell")
	}
	if command.Action != "search" && !hasID {
		return command, fmt.Errorf("links: %s requires exactly one id or name", command.Action)
	}
	return command, nil
}
