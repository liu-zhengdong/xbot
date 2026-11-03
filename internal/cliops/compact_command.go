package cliops

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type CompactCommand struct {
	From, To         int
	Focus, FocusFile string
	Self, JSON, Help bool
}

// ParseCompactRuntime consumes syntax only; args excludes compact.
// Value flags consume their next argument literally, including --help and --.
func ParseCompactRuntime(args []string) (CompactCommand, error) {
	var command CompactCommand
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		flag, value, equals := strings.Cut(args[i], "=")
		if seen[flag] {
			return command, fmt.Errorf("compact: repeated %s", flag)
		}
		seen[flag] = true
		switch flag {
		case "--self", "--json", "--help":
			if equals {
				return command, fmt.Errorf("compact: %s takes no value", flag)
			}
			switch flag {
			case "--self":
				command.Self = true
			case "--json":
				command.JSON = true
			case "--help":
				command.Help = true
			}
		case "--from", "--to", "--focus", "--focus-file":
			if !equals {
				if i+1 == len(args) {
					return command, fmt.Errorf("compact: %s needs a value", flag)
				}
				i++
				value = args[i]
			}
			switch flag {
			case "--focus":
				command.Focus = value
			case "--focus-file":
				command.FocusFile = value
			default:
				n, err := strconv.Atoi(value)
				if err != nil {
					return command, fmt.Errorf("compact: %s requires an integer", flag)
				}
				if flag == "--from" {
					command.From = n
				} else {
					command.To = n
				}
			}
		default:
			return command, fmt.Errorf("compact: unexpected argument %q", args[i])
		}
	}
	if seen["--focus"] && seen["--focus-file"] {
		return command, errors.New("compact: --focus and --focus-file are mutually exclusive")
	}
	if seen["--focus-file"] && command.FocusFile == "" {
		return command, errors.New("compact: --focus-file needs a nonempty path")
	}
	if command.Help {
		return command, nil
	}
	if !command.Self {
		return command, errors.New("compact: requires --self from an active XBot shell")
	}
	if !seen["--from"] || !seen["--to"] {
		return command, errors.New("compact: requires --from and --to")
	}
	return command, nil
}
