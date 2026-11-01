package cliops

import (
	"errors"
	"fmt"
	"strings"
)

type BotForkCommand struct {
	Name, Role, RoleFile, Group string
	Self, JSON, Help            bool
}

// ParseBotForkRuntime consumes syntax only; args excludes the bot-fork noun.
func ParseBotForkRuntime(args []string) (BotForkCommand, error) {
	var command BotForkCommand
	seen := map[string]bool{}
	var positional []string
	literal := false
	for i := 0; i < len(args); i++ {
		word := args[i]
		if literal || !strings.HasPrefix(word, "-") || word == "-" {
			positional = append(positional, word)
			continue
		}
		if word == "--" {
			literal = true
			continue
		}
		flag, value, equals := strings.Cut(word, "=")
		if seen[flag] {
			return command, fmt.Errorf("bot-fork: repeated %s", flag)
		}
		seen[flag] = true
		switch flag {
		case "--self", "--json", "--help":
			if equals {
				return command, fmt.Errorf("bot-fork: %s takes no value", flag)
			}
			switch flag {
			case "--self":
				command.Self = true
			case "--json":
				command.JSON = true
			case "--help":
				command.Help = true
			}
		case "--role", "--role-file", "--group":
			if !equals {
				if i+1 == len(args) {
					return command, fmt.Errorf("bot-fork: %s needs a value", flag)
				}
				i++
				value = args[i]
			}
			switch flag {
			case "--role":
				command.Role = value
			case "--role-file":
				command.RoleFile = value
			case "--group":
				command.Group = value
			}
		default:
			return command, fmt.Errorf("bot-fork: unknown flag %q", flag)
		}
	}
	if len(positional) > 1 || (len(positional) == 1 && positional[0] == "") {
		return command, errors.New("bot-fork: expected one NAME")
	}
	if len(positional) == 1 {
		command.Name = positional[0]
	}
	if seen["--role"] && seen["--role-file"] {
		return command, errors.New("bot-fork: exactly one of --role or --role-file is required")
	}
	if seen["--role-file"] && command.RoleFile == "" {
		return command, errors.New("bot-fork: --role-file needs a nonempty path")
	}
	if command.Help {
		return command, nil
	}
	if len(positional) != 1 {
		return command, errors.New("bot-fork: expected one NAME")
	}
	if !command.Self {
		return command, errors.New("bot-fork: requires --self from an active XBot shell")
	}
	if !seen["--role"] && !seen["--role-file"] {
		return command, errors.New("bot-fork: exactly one of --role or --role-file is required")
	}
	return command, nil
}
