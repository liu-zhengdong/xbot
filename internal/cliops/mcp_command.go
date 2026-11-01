package cliops

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type MCPCommand struct {
	Action, Server, Tool, Scope         string
	Self, JSON, ReadOnly, Help, Offline bool
	HTTP, Bot, Command, URL             string
	CommandArgs, SecretEnvs             []string
	Args, ArgsFile                      string
	Timeout                             time.Duration
	ArgsSet                             bool
}

// ParseMCP reads only argv. Target environment and argument-file IO belong to cmd.
func ParseMCP(argv []string) (MCPCommand, error) {
	c := MCPCommand{Scope: "global"}
	if len(argv) == 0 {
		return c, errors.New("usage: xbot mcp add|list|remove|tools|describe|call --self or --http ADDR --bot NAME")
	}
	if argv[0] == "offline" {
		c.Offline = true
		argv = argv[1:]
	}
	if len(argv) == 0 {
		return c, errors.New("mcp offline: expected list, add or remove")
	}
	if argv[0] == "--help" || argv[0] == "-h" {
		c.Help = true
		return c, nil
	}
	c.Action = argv[0]
	switch c.Action {
	case "list", "add", "remove", "tools", "describe", "call":
	default:
		return c, fmt.Errorf("mcp: unknown action %q", c.Action)
	}
	if c.Offline && c.Action != "list" && c.Action != "add" && c.Action != "remove" {
		return c, errors.New("mcp offline supports list/add/remove only")
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
		boolean := flag == "--self" || flag == "--json" || flag == "--readonly" || flag == "--help"
		repeat := flag == "--arg" || flag == "--secret-env"
		if seen[flag] && !repeat {
			return c, fmt.Errorf("mcp: repeated %s", flag)
		}
		seen[flag] = true
		if boolean {
			if eq {
				return c, fmt.Errorf("mcp: %s takes no value", flag)
			}
			switch flag {
			case "--self":
				c.Self = true
			case "--json":
				c.JSON = true
			case "--readonly":
				c.ReadOnly = true
			case "--help":
				c.Help = true
			}
			continue
		}
		switch flag {
		case "--scope", "--http", "--bot", "--command", "--url", "--arg", "--secret-env", "--args", "--args-file", "--timeout":
		default:
			return c, fmt.Errorf("mcp: unknown flag %q", flag)
		}
		if !eq {
			if i+1 == len(argv) {
				return c, fmt.Errorf("mcp: %s needs a value", flag)
			}
			i++
			value = argv[i]
		}
		if value == "" {
			return c, fmt.Errorf("mcp: %s needs a nonempty value", flag)
		}
		switch flag {
		case "--scope":
			c.Scope = value
		case "--http":
			c.HTTP = value
		case "--bot":
			c.Bot = value
		case "--command":
			c.Command = value
		case "--url":
			c.URL = value
		case "--arg":
			c.CommandArgs = append(c.CommandArgs, value)
		case "--secret-env":
			c.SecretEnvs = append(c.SecretEnvs, value)
		case "--args":
			c.Args = value
			c.ArgsSet = true
		case "--args-file":
			c.ArgsFile = value
		case "--timeout":
			var err error
			c.Timeout, err = time.ParseDuration(value)
			if err != nil || c.Timeout <= 0 {
				return c, errors.New("mcp: invalid --timeout")
			}
		}
	}
	if c.Help {
		return c, nil
	}
	if c.Scope != "global" && c.Scope != "bot" {
		return c, errors.New("mcp: --scope must be global or bot")
	}
	if c.Offline {
		if c.Self || c.HTTP != "" || c.Bot != "" || c.ReadOnly || seen["--scope"] || seen["--timeout"] {
			return c, errors.New("mcp offline cannot use runtime flags")
		}
		if len(pos) == 0 {
			return c, errors.New("mcp offline requires a bot")
		}
		c.Bot = pos[0]
		pos = pos[1:]
	} else if c.Self {
		if c.HTTP != "" || c.Bot != "" {
			return c, errors.New("mcp: --self cannot be mixed with --http/--bot")
		}
	} else if c.HTTP == "" || c.Bot == "" {
		return c, errors.New("mcp: specify --self or --http ADDR --bot NAME; file configuration uses mcp offline")
	}
	expected := 0
	switch c.Action {
	case "add", "remove", "tools":
		expected = 1
	case "describe", "call":
		expected = 2
	}
	if len(pos) != expected {
		return c, fmt.Errorf("mcp %s: expected %d name arguments; file configuration uses mcp offline", c.Action, expected)
	}
	if expected > 0 {
		c.Server = pos[0]
	}
	if expected > 1 {
		c.Tool = pos[1]
	}
	if c.Action == "add" {
		if (c.Command == "") == (c.URL == "") {
			return c, errors.New("mcp add requires exactly one of --command or --url")
		}
		if c.URL != "" && len(c.CommandArgs) > 0 {
			return c, errors.New("mcp: --arg requires --command")
		}
	} else if c.Command != "" || c.URL != "" || len(c.CommandArgs) > 0 || len(c.SecretEnvs) > 0 {
		return c, errors.New("mcp: transport flags require add")
	}
	if c.Action != "call" && (c.ArgsSet || c.ArgsFile != "" || c.ReadOnly) {
		return c, errors.New("mcp: args/readonly flags require call")
	}
	if c.ArgsSet && c.ArgsFile != "" {
		return c, errors.New("mcp: --args and --args-file are mutually exclusive")
	}
	if seen["--scope"] && c.Action != "list" && c.Action != "add" && c.Action != "remove" {
		return c, errors.New("mcp: --scope applies to configuration only")
	}
	return c, nil
}

func ValidateMCPArgs(raw json.RawMessage) error {
	if len(raw) == 0 || len(raw) > MCPMaxRequest {
		return errors.New("mcp: arguments must be one JSON object of at most 1 MiB")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return errors.New("mcp: arguments must be one JSON object")
	}
	return nil
}
