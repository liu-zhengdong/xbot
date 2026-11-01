package cliops

import (
	"errors"
	"fmt"
	"strings"
)

// SkillCommand contains only argv syntax. Runtime identity, body-file IO and
// domain validation belong to the command and source operation boundaries.
type SkillCommand struct {
	Action, ID, Slug, Title, Description, Scope, BodyFile string
	Body                                                  *string
	Self, JSON, Help                                      bool
}

func ParseSkillRuntime(argv []string) (SkillCommand, error) {
	var c SkillCommand
	if len(argv) == 0 {
		return c, errors.New("usage: xbot skill read|write --self; use skill --help")
	}
	if argv[0] == "help" || argv[0] == "--help" || argv[0] == "-h" {
		if len(argv) != 1 {
			return c, errors.New("skill help takes no arguments")
		}
		c.Help = true
		return c, nil
	}
	c.Action = argv[0]
	if c.Action != "read" && c.Action != "write" {
		return c, fmt.Errorf("skill: unknown runtime action %q", c.Action)
	}
	seen := map[string]bool{}
	var pos []string
	for i := 1; i < len(argv); i++ {
		word := argv[i]
		if !strings.HasPrefix(word, "-") {
			pos = append(pos, word)
			continue
		}
		flag, value, eq := strings.Cut(word, "=")
		if flag == "-h" {
			flag = "--help"
		}
		if seen[flag] {
			return c, fmt.Errorf("skill: repeated %s", flag)
		}
		seen[flag] = true
		switch flag {
		case "--self", "--json", "--help":
			if eq {
				return c, fmt.Errorf("skill: %s takes no value", flag)
			}
			switch flag {
			case "--self":
				c.Self = true
			case "--json":
				c.JSON = true
			case "--help":
				c.Help = true
			}
		case "--title", "--description", "--scope", "--body", "--body-file":
			if c.Action != "write" {
				return c, fmt.Errorf("skill: %s requires write", flag)
			}
			if !eq {
				if i+1 == len(argv) {
					return c, fmt.Errorf("skill: %s needs a value", flag)
				}
				i++
				value = argv[i]
			}
			switch flag {
			case "--title":
				c.Title = value
			case "--description":
				c.Description = value
			case "--scope":
				c.Scope = value
			case "--body":
				c.Body = &value
			case "--body-file":
				if value == "" {
					return c, errors.New("skill: --body-file needs a nonempty path or -")
				}
				c.BodyFile = value
			}
		default:
			return c, fmt.Errorf("skill: unknown flag %q", flag)
		}
	}
	if seen["--body"] && seen["--body-file"] {
		return c, errors.New("skill: --body and --body-file are mutually exclusive")
	}
	if c.Help {
		return c, nil
	}
	if !c.Self {
		return c, errors.New("skill: runtime read/write require --self from an active XBot shell")
	}
	if len(pos) != 1 || pos[0] == "" {
		return c, fmt.Errorf("skill %s: expected one skill identifier", c.Action)
	}
	if c.Action == "read" {
		c.ID = pos[0]
		return c, nil
	}
	c.Slug = pos[0]
	if !seen["--description"] {
		return c, errors.New("skill write: --description is required")
	}
	if c.Body == nil && !seen["--body-file"] {
		return c, errors.New("skill write: exactly one of --body or --body-file is required")
	}
	return c, nil
}
