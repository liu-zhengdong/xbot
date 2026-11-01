package cliops

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type ExperienceCommand struct {
	Action                                 string
	Summary, SummaryFile, Focus, FocusFile string
	From, To                               int
	Query, QueryFile                       string
	Limit                                  int
	Self, JSON, Help                       bool
}

// ParseExperienceRuntime consumes syntax only; args excludes the experience noun.
// Value flags consume their next argument literally, including --help and --.
func ParseExperienceRuntime(args []string) (ExperienceCommand, error) {
	var command ExperienceCommand
	if len(args) > 0 && (args[0] == "record" || args[0] == "search") {
		command.Action, args = args[0], args[1:]
	}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		flag, value, equals := strings.Cut(args[i], "=")
		if seen[flag] {
			return command, fmt.Errorf("experience: repeated %s", flag)
		}
		seen[flag] = true
		switch flag {
		case "--self", "--json", "--help":
			if equals {
				return command, fmt.Errorf("experience: %s takes no value", flag)
			}
			switch flag {
			case "--self":
				command.Self = true
			case "--json":
				command.JSON = true
			case "--help":
				command.Help = true
			}
		case "--summary", "--summary-file", "--focus", "--focus-file", "--from", "--to", "--query", "--query-file", "--limit":
			recordFlag := flag == "--summary" || flag == "--summary-file" || flag == "--focus" || flag == "--focus-file" || flag == "--from" || flag == "--to"
			if (recordFlag && command.Action != "record") || (!recordFlag && command.Action != "search") {
				return command, fmt.Errorf("experience: %s is not valid for %q", flag, command.Action)
			}
			if !equals {
				if i+1 == len(args) {
					return command, fmt.Errorf("experience: %s needs a value", flag)
				}
				i++
				value = args[i]
			}
			switch flag {
			case "--summary":
				command.Summary = value
			case "--summary-file":
				command.SummaryFile = value
			case "--focus":
				command.Focus = value
			case "--focus-file":
				command.FocusFile = value
			case "--query":
				command.Query = value
			case "--query-file":
				command.QueryFile = value
			default:
				n, err := strconv.Atoi(value)
				if err != nil {
					return command, fmt.Errorf("experience: %s requires an integer", flag)
				}
				switch flag {
				case "--from":
					command.From = n
				case "--to":
					command.To = n
				case "--limit":
					command.Limit = n
				}
			}
		default:
			return command, fmt.Errorf("experience: unexpected argument %q", args[i])
		}
	}
	stdin := 0
	for _, pair := range [][2]string{{"--summary", "--summary-file"}, {"--focus", "--focus-file"}, {"--query", "--query-file"}} {
		if seen[pair[0]] && seen[pair[1]] {
			return command, fmt.Errorf("experience: %s and %s are mutually exclusive", pair[0], pair[1])
		}
		var path string
		switch pair[1] {
		case "--summary-file":
			path = command.SummaryFile
		case "--focus-file":
			path = command.FocusFile
		case "--query-file":
			path = command.QueryFile
		}
		if seen[pair[1]] && path == "" {
			return command, fmt.Errorf("experience: %s needs a nonempty path", pair[1])
		}
		if path == "-" {
			stdin++
		}
	}
	if stdin > 1 {
		return command, errors.New("experience: stdin may supply only one field")
	}
	if command.Help {
		return command, nil
	}
	if command.Action == "" {
		return command, errors.New("experience: expected record or search")
	}
	if !command.Self {
		return command, errors.New("experience: requires --self from an active XBot shell")
	}
	if command.Action == "record" && !seen["--summary"] && !seen["--summary-file"] {
		return command, errors.New("experience: exactly one of --summary or --summary-file is required")
	}
	return command, nil
}
