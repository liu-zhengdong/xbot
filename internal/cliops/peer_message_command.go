package cliops

import (
	"errors"
	"fmt"
	"strings"
)

type PeerMessageCommand struct {
	Bot, Text, TextFile string
	Self, JSON, Help    bool
}

// ParsePeerMessageRuntime consumes syntax only; args excludes peer-message.
func ParsePeerMessageRuntime(args []string) (PeerMessageCommand, error) {
	var command PeerMessageCommand
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
		if flag == "-h" {
			flag = "--help"
		}
		if seen[flag] {
			return command, fmt.Errorf("peer-message: repeated %s", flag)
		}
		seen[flag] = true
		switch flag {
		case "--self", "--json", "--help":
			if equals {
				return command, fmt.Errorf("peer-message: %s takes no value", flag)
			}
			switch flag {
			case "--self":
				command.Self = true
			case "--json":
				command.JSON = true
			case "--help":
				command.Help = true
			}
		case "--text", "--file":
			if !equals {
				if i+1 == len(args) {
					return command, fmt.Errorf("peer-message: %s needs a value", flag)
				}
				i++
				value = args[i]
			}
			if flag == "--text" {
				command.Text = value
			} else {
				command.TextFile = value
			}
		default:
			return command, fmt.Errorf("peer-message: unknown flag %q", flag)
		}
	}
	if len(positional) > 1 || (len(positional) == 1 && positional[0] == "") {
		return command, errors.New("peer-message: expected one TARGET")
	}
	if len(positional) == 1 {
		command.Bot = positional[0]
	}
	if seen["--text"] && seen["--file"] {
		return command, errors.New("peer-message: exactly one of --text or --file is required")
	}
	if seen["--file"] && command.TextFile == "" {
		return command, errors.New("peer-message: --file needs a nonempty path")
	}
	if command.Help {
		return command, nil
	}
	if len(positional) != 1 {
		return command, errors.New("peer-message: expected one TARGET")
	}
	if !command.Self {
		return command, errors.New("peer-message: requires --self from an active XBot shell")
	}
	if !seen["--text"] && !seen["--file"] {
		return command, errors.New("peer-message: exactly one of --text or --file is required")
	}
	return command, nil
}
