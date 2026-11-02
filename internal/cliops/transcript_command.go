package cliops

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type TranscriptCommand struct {
	Action, Query, QueryFile, Bot string
	From, To, Seq                 int
	Experience                    int64
	Self, JSON, Help              bool
}

// ParseTranscriptRuntime consumes syntax only; args excludes transcript.
// Value flags consume the next argument literally, including --help and --.
func ParseTranscriptRuntime(args []string) (TranscriptCommand, error) {
	var command TranscriptCommand
	if len(args) > 0 && (args[0] == "search" || args[0] == "expand" || args[0] == "detail") {
		command.Action, args = args[0], args[1:]
	}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		flag, value, equals := strings.Cut(args[i], "=")
		if seen[flag] {
			return command, fmt.Errorf("transcript: repeated %s", flag)
		}
		seen[flag] = true
		switch flag {
		case "--self", "--json", "--help":
			if equals {
				return command, fmt.Errorf("transcript: %s takes no value", flag)
			}
			switch flag {
			case "--self":
				command.Self = true
			case "--json":
				command.JSON = true
			case "--help":
				command.Help = true
			}
		case "--query", "--query-file", "--bot", "--from", "--to", "--seq", "--experience":
			valid := (command.Action == "search" && (flag == "--query" || flag == "--query-file" || flag == "--bot")) ||
				(command.Action == "expand" && (flag == "--bot" || flag == "--from" || flag == "--to")) ||
				(command.Action == "detail" && (flag == "--seq" || flag == "--experience"))
			if !valid {
				return command, fmt.Errorf("transcript: %s is not valid for %q", flag, command.Action)
			}
			if !equals {
				if i+1 == len(args) {
					return command, fmt.Errorf("transcript: %s needs a value", flag)
				}
				i++
				value = args[i]
			}
			switch flag {
			case "--query":
				command.Query = value
			case "--query-file":
				command.QueryFile = value
			case "--bot":
				command.Bot = value
			case "--experience":
				n, err := strconv.ParseInt(value, 10, 64)
				if err != nil {
					return command, errors.New("transcript: --experience requires an int64 integer")
				}
				command.Experience = n
			default:
				n, err := strconv.Atoi(value)
				if err != nil {
					return command, fmt.Errorf("transcript: %s requires an integer", flag)
				}
				switch flag {
				case "--from":
					command.From = n
				case "--to":
					command.To = n
				case "--seq":
					command.Seq = n
				}
			}
		default:
			return command, fmt.Errorf("transcript: unexpected argument %q", args[i])
		}
	}
	if seen["--query"] && seen["--query-file"] {
		return command, errors.New("transcript: --query and --query-file are mutually exclusive")
	}
	if seen["--query-file"] && command.QueryFile == "" {
		return command, errors.New("transcript: --query-file needs a nonempty path")
	}
	if command.Help {
		return command, nil
	}
	if command.Action == "" {
		return command, errors.New("transcript: expected search, expand or detail")
	}
	if !command.Self {
		return command, errors.New("transcript: requires --self from an active XBot shell")
	}
	if command.Action == "expand" && (!seen["--from"] || !seen["--to"]) {
		return command, errors.New("transcript: expand requires --from and --to")
	}
	if command.Action == "detail" && !seen["--seq"] {
		return command, errors.New("transcript: detail requires --seq")
	}
	return command, nil
}
