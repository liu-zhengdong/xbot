package cliops

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type DeviceCommand struct {
	Action, DeviceID, Ref, Tool, Description, Args, ArgsFile, ArtifactID string
	Offset, Limit                                                        int
	Self, JSON, Help                                                     bool
}

// ParseDeviceRuntime parses syntax only. args excludes the device noun.
func ParseDeviceRuntime(args []string) (DeviceCommand, error) {
	var c DeviceCommand
	if len(args) == 0 {
		return c, errors.New("device: expected list, exec, read, write, desktop, browser, tasks, task, stop, or artifact")
	}
	switch args[0] {
	case "list", "exec", "read", "write", "desktop", "browser", "tasks", "task", "stop", "artifact":
		c.Action, args = args[0], args[1:]
	case "--help", "-h":
	default:
		return c, fmt.Errorf("device: unknown action %q", args[0])
	}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		if !strings.HasPrefix(args[i], "-") && (c.Action == "task" || c.Action == "stop" || c.Action == "artifact") && !seen["--ref"] {
			c.Ref = args[i]
			seen["--ref"] = true
			continue
		}
		flag, value, equals := strings.Cut(args[i], "=")
		if flag == "-h" {
			flag = "--help"
		}
		if seen[flag] {
			return c, fmt.Errorf("device: repeated %s", flag)
		}
		seen[flag] = true
		switch flag {
		case "--self", "--json", "--help":
			if equals {
				return c, fmt.Errorf("device: %s takes no value", flag)
			}
			switch flag {
			case "--self":
				c.Self = true
			case "--json":
				c.JSON = true
			case "--help":
				c.Help = true
			}
		case "--device", "--ref", "--tool", "--description", "--args", "--args-file", "--artifact-id", "--offset", "--limit":
			call := c.Action == "exec" || c.Action == "read" || c.Action == "write" || c.Action == "desktop" || c.Action == "browser"
			if (flag == "--tool" || flag == "--description" || flag == "--args" || flag == "--args-file") && !call {
				return c, fmt.Errorf("device: %s is not valid for %s", flag, c.Action)
			}
			if flag == "--artifact-id" && c.Action != "artifact" {
				return c, errors.New("device: --artifact-id requires artifact")
			}
			if (flag == "--offset" || flag == "--limit") && c.Action != "tasks" && c.Action != "task" && c.Action != "artifact" {
				return c, fmt.Errorf("device: %s is not valid for %s", flag, c.Action)
			}
			if !equals {
				if i+1 == len(args) {
					return c, fmt.Errorf("device: %s needs a value", flag)
				}
				i++
				value = args[i]
			}
			if value == "" {
				return c, fmt.Errorf("device: %s needs a nonempty value", flag)
			}
			switch flag {
			case "--device":
				c.DeviceID = value
			case "--ref":
				c.Ref = value
			case "--tool":
				c.Tool = value
			case "--description":
				c.Description = value
			case "--args":
				c.Args = value
			case "--args-file":
				c.ArgsFile = value
			case "--artifact-id":
				c.ArtifactID = value
			case "--offset", "--limit":
				n, err := strconv.Atoi(value)
				if err != nil || n < 0 {
					return c, fmt.Errorf("device: %s needs a nonnegative integer", flag)
				}
				if flag == "--offset" {
					c.Offset = n
				} else {
					c.Limit = n
				}
			}
		default:
			return c, fmt.Errorf("device: unexpected argument %q", args[i])
		}
	}
	if seen["--args"] && seen["--args-file"] {
		return c, errors.New("device: --args and --args-file are mutually exclusive")
	}
	if c.Help {
		return c, nil
	}
	if !c.Self {
		return c, errors.New("device: requires --self from an active XBot shell")
	}
	switch c.Action {
	case "exec", "read", "write", "desktop", "browser":
		if !seen["--args"] && !seen["--args-file"] {
			return c, errors.New("device: call requires --args or --args-file")
		}
		if c.Tool == "" {
			switch c.Action {
			case "exec": /* The target chooses its default interpreter. */
			case "read":
				c.Tool = "read_file"
			case "write":
				c.Tool = "write_file"
			default:
				return c, errors.New("device: desktop/browser requires --tool")
			}
		}
	case "task", "stop", "artifact":
		if c.Ref == "" {
			return c, errors.New("device: task reference is required")
		}
		if c.Action == "artifact" && c.ArtifactID == "" {
			return c, errors.New("device: artifact requires --artifact-id")
		}
	}
	return c, nil
}
