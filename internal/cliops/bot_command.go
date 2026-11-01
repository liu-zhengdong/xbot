package cliops

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// BotCommand contains syntax only. Offline preserves the existing flat commands;
// it never depends on environment, data-root contents or a running endpoint.
type BotCommand struct {
	Action, Name, Persona, PersonaFile string
	Steps                              int
	Self, JSON, Help, Offline          bool
}

func ParseBotRuntime(argv []string) (BotCommand, error) {
	var c BotCommand
	if len(argv) == 0 {
		return c, errors.New("bot: expected list, info, new, delete, pin, unpin or primary")
	}
	c.Action = argv[0]
	switch c.Action {
	case "list", "info", "new", "delete", "pin", "unpin", "primary":
	default:
		return c, fmt.Errorf("bot: unknown command %q", c.Action)
	}
	control := c.Action == "pin" || c.Action == "unpin" || c.Action == "primary"
	c.Offline = c.Action != "info" && !control
	// Identify runtime syntax with the original new flag arities. A value or a
	// positional after -- may literally be --self or --help.
	for i := 1; i < len(argv); i++ {
		flag, _, eq := strings.Cut(argv[i], "=")
		if flag == "--" {
			break
		}
		switch flag {
		case "--self", "--json", "--help", "-h", "--persona-file", "--steps":
			c.Offline = false
		case "--persona", "--provider", "--model", "-persona", "-provider", "-model":
			if c.Action == "new" && !eq {
				i++
			}
		}
	}
	if c.Offline {
		return c, nil
	}
	seen := map[string]bool{}
	var pos []string
	literal := false
	for i := 1; i < len(argv); i++ {
		word := argv[i]
		if literal || !strings.HasPrefix(word, "-") || word == "-" {
			pos = append(pos, word)
			continue
		}
		if word == "--" {
			literal = true
			continue
		}
		flag, value, eq := strings.Cut(word, "=")
		if flag == "-h" {
			flag = "--help"
		}
		if seen[flag] {
			return c, fmt.Errorf("bot: repeated %s", flag)
		}
		seen[flag] = true
		switch flag {
		case "--self", "--json", "--help":
			if eq {
				return c, fmt.Errorf("bot: %s takes no value", flag)
			}
			switch flag {
			case "--self":
				c.Self = true
			case "--json":
				c.JSON = true
			case "--help":
				c.Help = true
			}
		case "--persona", "--persona-file", "--steps", "--provider", "--model", "-persona", "-provider", "-model":
			if !eq {
				if i+1 == len(argv) {
					return c, fmt.Errorf("bot: %s needs a value", flag)
				}
				i++
				value = argv[i]
			}
			switch flag {
			case "--persona", "--persona-file":
				if c.Action != "new" {
					return c, fmt.Errorf("bot: %s requires new", flag)
				}
				if flag == "--persona" {
					c.Persona = value
				} else {
					c.PersonaFile = value
				}
			case "--steps":
				if c.Action != "info" {
					return c, errors.New("bot: --steps requires info")
				}
				n, err := strconv.Atoi(value)
				if err != nil {
					return c, errors.New("bot: --steps requires an integer")
				}
				c.Steps = n
			default:
				return c, fmt.Errorf("bot: %s is only available for offline new", flag)
			}
		default:
			return c, fmt.Errorf("bot: unknown flag %q", flag)
		}
	}
	if seen["--persona"] && seen["--persona-file"] {
		return c, errors.New("bot: --persona and --persona-file are mutually exclusive")
	}
	if c.Help {
		return c, nil
	}
	if control && !c.Self && !c.JSON {
		switch c.Action {
		case "pin", "unpin":
			if len(pos) != 1 || pos[0] == "" {
				return c, fmt.Errorf("bot %s: expected one Bot name", c.Action)
			}
		case "primary":
			if len(pos) != 0 && (len(pos) != 2 || pos[0] != "set" || pos[1] == "") {
				return c, errors.New("bot primary: expected no arguments or set NAME")
			}
		}
		c.Offline = true
		return c, nil
	}
	if !c.Self {
		return c, errors.New("bot: runtime commands require --self from an active XBot shell")
	}
	switch c.Action {
	case "list":
		if len(pos) != 0 {
			return c, errors.New("bot list: takes no positional arguments")
		}
	case "info":
		if len(pos) > 1 {
			return c, errors.New("bot info: expected at most one Bot name")
		}
		if len(pos) == 1 {
			c.Name = pos[0]
		}
	case "primary":
		if len(pos) != 2 || pos[0] != "set" || pos[1] == "" {
			return c, errors.New("bot primary: runtime requires set NAME")
		}
		c.Name = pos[1]
	case "new", "delete", "pin", "unpin":
		if len(pos) != 1 || pos[0] == "" {
			return c, fmt.Errorf("bot %s: expected one Bot name", c.Action)
		}
		c.Name = pos[0]
	}
	if c.Action == "new" {
		if !seen["--persona"] && !seen["--persona-file"] {
			return c, errors.New("bot new: exactly one of --persona or --persona-file is required")
		}
		if seen["--persona-file"] && c.PersonaFile == "" {
			return c, errors.New("bot new: --persona-file needs a nonempty path")
		}
	}
	return c, nil
}
