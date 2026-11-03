package cliops

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type SubagentCommand struct {
	Action, ID       string
	Limit, From, To  int
	Self, JSON, Help bool
}

// ParseSubagentRuntime consumes syntax only; args excludes subagent. The noun
// level accepts exactly list, read, and expand, each with the same strict flag
// layer as ParseLinksRuntime: no aliases, value flags consume the next argument
// literally, and help stays offline at both levels.
func ParseSubagentRuntime(args []string) (SubagentCommand, error) {
	var command SubagentCommand
	if len(args) == 0 {
		return command, errors.New("subagent: expected list, read, or expand")
	}
	switch args[0] {
	case "list", "read", "expand":
		command.Action, args = args[0], args[1:]
	default:
		if args[0] != "--help" && args[0] != "-h" {
			return command, fmt.Errorf("subagent: unexpected subcommand %q", args[0])
		}
	}
	seen := map[string]bool{}
	hasID := false
	for i := 0; i < len(args); i++ {
		if (command.Action == "read" || command.Action == "expand") && !strings.HasPrefix(args[i], "-") {
			if hasID {
				return command, fmt.Errorf("subagent: unexpected argument %q", args[i])
			}
			hasID = true
			command.ID = args[i]
			continue
		}
		flag, value, equals := strings.Cut(args[i], "=")
		if seen[flag] {
			return command, fmt.Errorf("subagent: repeated %s", flag)
		}
		seen[flag] = true
		switch flag {
		case "--self", "--json", "--help":
			if equals {
				return command, fmt.Errorf("subagent: %s takes no value", flag)
			}
			switch flag {
			case "--self":
				command.Self = true
			case "--json":
				command.JSON = true
			case "--help":
				command.Help = true
			}
		case "--limit", "--from", "--to":
			valid := (command.Action == "list" && flag == "--limit") ||
				(command.Action == "expand" && (flag == "--from" || flag == "--to"))
			if !valid {
				return command, fmt.Errorf("subagent: %s is not valid for %q", flag, command.Action)
			}
			if !equals {
				if i+1 == len(args) {
					return command, fmt.Errorf("subagent: %s needs a value", flag)
				}
				i++
				value = args[i]
			}
			n, err := strconv.Atoi(value)
			if err != nil {
				return command, fmt.Errorf("subagent: %s requires an integer", flag)
			}
			switch flag {
			case "--limit":
				command.Limit = n
			case "--from":
				command.From = n
			case "--to":
				command.To = n
			}
		default:
			return command, fmt.Errorf("subagent: unexpected argument %q", args[i])
		}
	}
	if command.Help {
		return command, nil
	}
	if command.Action == "" {
		return command, errors.New("subagent: expected list, read, or expand")
	}
	if !command.Self {
		return command, errors.New("subagent: requires --self from an active XBot shell")
	}
	if (command.Action == "read" || command.Action == "expand") && !hasID {
		return command, fmt.Errorf("subagent: %s requires exactly one id", command.Action)
	}
	return command, nil
}
