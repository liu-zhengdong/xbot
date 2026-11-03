package cliops

import (
	"errors"
	"fmt"
	"strings"
)

type RestartCommand struct {
	Hint             string
	Self, JSON, Help bool
}

// ParseRestartRuntime consumes syntax only; args excludes restart. Value flags
// consume their next argument literally; help stays offline.
func ParseRestartRuntime(args []string) (RestartCommand, error) {
	var command RestartCommand
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		flag, value, equals := strings.Cut(args[i], "=")
		if seen[flag] {
			return command, fmt.Errorf("restart: repeated %s", flag)
		}
		seen[flag] = true
		switch flag {
		case "--self", "--json", "--help":
			if equals {
				return command, fmt.Errorf("restart: %s takes no value", flag)
			}
			switch flag {
			case "--self":
				command.Self = true
			case "--json":
				command.JSON = true
			case "--help":
				command.Help = true
			}
		case "--hint":
			if !equals {
				if i+1 == len(args) {
					return command, fmt.Errorf("restart: %s needs a value", flag)
				}
				i++
				value = args[i]
			}
			command.Hint = value
		default:
			return command, fmt.Errorf("restart: unexpected argument %q", args[i])
		}
	}
	if command.Help {
		return command, nil
	}
	if !command.Self {
		return command, errors.New("restart: requires --self from an active XBot shell")
	}
	return command, nil
}

type HTTPCommand struct {
	URL, Method      string
	Headers          map[string]string
	Body, BodyFile   string
	Self, JSON, Help bool
}

// ParseHTTPRuntime consumes syntax only; args excludes http. Value flags
// consume their next argument literally; --header repeats as K=V with unique
// non-empty keys; help stays offline.
func ParseHTTPRuntime(args []string) (HTTPCommand, error) {
	var command HTTPCommand
	seen := map[string]bool{}
	positional := 0
	for i := 0; i < len(args); i++ {
		if !strings.HasPrefix(args[i], "-") {
			positional++
			if positional > 1 {
				return command, fmt.Errorf("http: unexpected argument %q", args[i])
			}
			command.URL = args[i]
			continue
		}
		flag, value, equals := strings.Cut(args[i], "=")
		if flag != "--header" && seen[flag] {
			return command, fmt.Errorf("http: repeated %s", flag)
		}
		seen[flag] = true
		switch flag {
		case "--self", "--json", "--help":
			if equals {
				return command, fmt.Errorf("http: %s takes no value", flag)
			}
			switch flag {
			case "--self":
				command.Self = true
			case "--json":
				command.JSON = true
			case "--help":
				command.Help = true
			}
		case "--method", "--body", "--body-file", "--header":
			if !equals {
				if i+1 == len(args) {
					return command, fmt.Errorf("http: %s needs a value", flag)
				}
				i++
				value = args[i]
			}
			switch flag {
			case "--method":
				command.Method = value
			case "--body":
				command.Body = value
			case "--body-file":
				command.BodyFile = value
			case "--header":
				name, val, ok := strings.Cut(value, "=")
				if !ok || strings.TrimSpace(name) == "" {
					return command, fmt.Errorf("http: --header requires K=V")
				}
				if command.Headers == nil {
					command.Headers = map[string]string{}
				}
				if _, dup := command.Headers[name]; dup {
					return command, fmt.Errorf("http: repeated header %q", name)
				}
				command.Headers[name] = val
			}
		default:
			return command, fmt.Errorf("http: unexpected argument %q", args[i])
		}
	}
	if seen["--body"] && seen["--body-file"] {
		return command, errors.New("http: --body and --body-file are mutually exclusive")
	}
	if seen["--body-file"] && command.BodyFile == "" {
		return command, errors.New("http: --body-file needs a nonempty path")
	}
	if command.Help {
		return command, nil
	}
	if !command.Self {
		return command, errors.New("http: requires --self from an active XBot shell")
	}
	if positional != 1 {
		return command, errors.New("http: requires exactly one url")
	}
	return command, nil
}
