package cliops

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type JobsCommand struct {
	Action, ID       string
	Offset, Tail     int
	Self, JSON, Help bool
}

// ParseJobsRuntime consumes syntax only; args excludes jobs. The noun level
// accepts exactly output, status, and cancel, each with the same strict flag
// layer as ParseSubagentRuntime: no aliases, value flags consume the next
// argument literally, and help stays offline at both levels. output and
// cancel take exactly one positional id, status takes zero or one, and
// --offset/--tail are only valid for output.
func ParseJobsRuntime(args []string) (JobsCommand, error) {
	var command JobsCommand
	if len(args) == 0 {
		return command, errors.New("jobs: expected output, status, or cancel")
	}
	switch args[0] {
	case "output", "status", "cancel":
		command.Action, args = args[0], args[1:]
	default:
		if args[0] != "--help" && args[0] != "-h" {
			return command, fmt.Errorf("jobs: unexpected subcommand %q", args[0])
		}
	}
	seen := map[string]bool{}
	hasID := false
	for i := 0; i < len(args); i++ {
		if command.Action != "" && !strings.HasPrefix(args[i], "-") {
			if hasID {
				return command, fmt.Errorf("jobs: unexpected argument %q", args[i])
			}
			hasID = true
			command.ID = args[i]
			continue
		}
		flag, value, equals := strings.Cut(args[i], "=")
		if seen[flag] {
			return command, fmt.Errorf("jobs: repeated %s", flag)
		}
		seen[flag] = true
		switch flag {
		case "--self", "--json", "--help":
			if equals {
				return command, fmt.Errorf("jobs: %s takes no value", flag)
			}
			switch flag {
			case "--self":
				command.Self = true
			case "--json":
				command.JSON = true
			case "--help":
				command.Help = true
			}
		case "--offset", "--tail":
			if command.Action != "output" {
				return command, fmt.Errorf("jobs: %s is not valid for %q", flag, command.Action)
			}
			if !equals {
				if i+1 == len(args) {
					return command, fmt.Errorf("jobs: %s needs a value", flag)
				}
				i++
				value = args[i]
			}
			n, err := strconv.Atoi(value)
			if err != nil {
				return command, fmt.Errorf("jobs: %s requires an integer", flag)
			}
			if flag == "--offset" {
				command.Offset = n
			} else {
				command.Tail = n
			}
		default:
			return command, fmt.Errorf("jobs: unexpected argument %q", args[i])
		}
	}
	if command.Help {
		return command, nil
	}
	if command.Action == "" {
		return command, errors.New("jobs: expected output, status, or cancel")
	}
	if !command.Self {
		return command, errors.New("jobs: requires --self from an active XBot shell")
	}
	if (command.Action == "output" || command.Action == "cancel") && !hasID {
		return command, fmt.Errorf("jobs: %s requires exactly one id", command.Action)
	}
	return command, nil
}
