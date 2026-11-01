package cliops

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type ChatGroupCommand struct {
	Action, ID, Name, Text, TextFile string
	Members                          []string
	Limit                            int
	Keyword, Sender, BeforeID        string
	Self, JSON, Help                 bool
}

// ParseChatGroupRuntime consumes only syntax, with args excluding chat-group.
func ParseChatGroupRuntime(args []string) (ChatGroupCommand, error) {
	var c ChatGroupCommand
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		c.Help = true
		return c, nil
	}
	if len(args) == 0 {
		return c, errors.New("chat-group: expected list, history, create or post")
	}
	c.Action = args[0]
	switch c.Action {
	case "list", "history", "create", "post":
	default:
		return c, fmt.Errorf("chat-group: unknown command %q", c.Action)
	}
	seen := map[string]bool{}
	var pos []string
	literal := false
	for i := 1; i < len(args); i++ {
		word := args[i]
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
		if seen[flag] && flag != "--member" {
			return c, fmt.Errorf("chat-group: repeated %s", flag)
		}
		seen[flag] = true
		switch flag {
		case "--self", "--json", "--help":
			if eq {
				return c, fmt.Errorf("chat-group: %s takes no value", flag)
			}
			switch flag {
			case "--self":
				c.Self = true
			case "--json":
				c.JSON = true
			case "--help":
				c.Help = true
			}
		case "--limit", "--before-id", "--sender", "--keyword", "--member", "--text", "--file":
			if !eq {
				if i+1 == len(args) {
					return c, fmt.Errorf("chat-group: %s needs a value", flag)
				}
				i++
				value = args[i]
			}
			switch flag {
			case "--member":
				if c.Action != "create" {
					return c, errors.New("chat-group: --member requires create")
				}
				c.Members = append(c.Members, value)
			case "--text", "--file":
				if c.Action != "post" {
					return c, fmt.Errorf("chat-group: %s requires post", flag)
				}
				if flag == "--text" {
					c.Text = value
				} else {
					c.TextFile = value
				}
			default:
				if c.Action != "history" {
					return c, fmt.Errorf("chat-group: %s requires history", flag)
				}
				switch flag {
				case "--limit":
					n, err := strconv.Atoi(value)
					if err != nil {
						return c, errors.New("chat-group: --limit requires an integer")
					}
					c.Limit = n
				case "--before-id":
					c.BeforeID = value
				case "--sender":
					c.Sender = value
				case "--keyword":
					c.Keyword = value
				}
			}
		default:
			return c, fmt.Errorf("chat-group: unknown flag %q", flag)
		}
	}
	if seen["--text"] && seen["--file"] {
		return c, errors.New("chat-group post: --text and --file are mutually exclusive")
	}
	if c.Action == "list" {
		if len(pos) != 0 {
			return c, errors.New("chat-group list: takes no positional arguments")
		}
	} else {
		if len(pos) > 1 || (len(pos) == 1 && pos[0] == "") {
			return c, fmt.Errorf("chat-group %s: expected one positional argument", c.Action)
		}
		if len(pos) == 1 {
			if c.Action == "create" {
				c.Name = pos[0]
			} else {
				c.ID = pos[0]
			}
		}
	}
	if c.Help {
		return c, nil
	}
	if !c.Self {
		return c, errors.New("chat-group: requires --self from an active XBot shell")
	}
	if c.Action != "list" && len(pos) != 1 {
		return c, fmt.Errorf("chat-group %s: expected one positional argument", c.Action)
	}
	if c.Action == "create" && len(c.Members) == 0 {
		return c, errors.New("chat-group create: requires at least one --member")
	}
	if c.Action == "post" {
		if !seen["--text"] && !seen["--file"] {
			return c, errors.New("chat-group post: exactly one of --text or --file is required")
		}
		if seen["--file"] && c.TextFile == "" {
			return c, errors.New("chat-group post: --file needs a nonempty path")
		}
	}
	return c, nil
}
