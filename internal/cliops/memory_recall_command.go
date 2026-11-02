package cliops

import (
	"errors"
	"fmt"
	"strings"
)

type MemoryRecallCommand struct {
	Query, QueryFile, Scope, Since string
	Self, JSON, Help               bool
}

// ParseMemoryRecallRuntime consumes syntax only; args excludes memory-recall.
// Value flags consume their next argument literally, including --help and --.
func ParseMemoryRecallRuntime(args []string) (MemoryRecallCommand, error) {
	var command MemoryRecallCommand
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		flag, value, equals := strings.Cut(args[i], "=")
		if seen[flag] {
			return command, fmt.Errorf("memory-recall: repeated %s", flag)
		}
		seen[flag] = true
		switch flag {
		case "--self", "--json", "--help":
			if equals {
				return command, fmt.Errorf("memory-recall: %s takes no value", flag)
			}
			switch flag {
			case "--self":
				command.Self = true
			case "--json":
				command.JSON = true
			case "--help":
				command.Help = true
			}
		case "--query", "--query-file", "--scope", "--since":
			if !equals {
				if i+1 == len(args) {
					return command, fmt.Errorf("memory-recall: %s needs a value", flag)
				}
				i++
				value = args[i]
			}
			switch flag {
			case "--query":
				command.Query = value
			case "--query-file":
				command.QueryFile = value
			case "--scope":
				command.Scope = value
			case "--since":
				command.Since = value
			}
		default:
			return command, fmt.Errorf("memory-recall: unexpected argument %q", args[i])
		}
	}
	if seen["--query"] && seen["--query-file"] {
		return command, errors.New("memory-recall: --query and --query-file are mutually exclusive")
	}
	if seen["--query-file"] && command.QueryFile == "" {
		return command, errors.New("memory-recall: --query-file needs a nonempty path")
	}
	if command.Help {
		return command, nil
	}
	if !command.Self {
		return command, errors.New("memory-recall: requires --self from an active XBot shell")
	}
	return command, nil
}
