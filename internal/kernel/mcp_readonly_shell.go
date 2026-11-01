package kernel

import (
	"encoding/json"
	"strings"

	"xbot/internal/cliops"
	"xbot/internal/review"
)

func (k *Kernel) allowReadOnlyPlatform(interpreter string, args json.RawMessage) bool {
	if k == nil {
		return false
	}
	switch interpreter {
	case "sh", "bash", "zsh", "git-bash":
	default:
		return false
	}
	k.operations.mu.Lock()
	binary, root, active := k.operations.binary, k.operations.root, k.operations.server != nil
	k.operations.mu.Unlock()
	if !active {
		return false
	}
	command, _ := review.ShellInvocation(args)
	argv, ok := review.DirectXBotArgs(command, binary)
	if !ok {
		return false
	}
	if len(argv) > 0 && argv[0] == "--root" {
		if len(argv) < 3 || argv[1] != root {
			return false
		}
		argv = argv[2:]
	} else if len(argv) > 0 && strings.HasPrefix(argv[0], "--root=") {
		if strings.TrimPrefix(argv[0], "--root=") != root {
			return false
		}
		argv = argv[1:]
	}
	if len(argv) == 0 {
		return false
	}
	if argv[0] == "list" || argv[0] == "info" || argv[0] == "new" || argv[0] == "delete" {
		parsed, err := cliops.ParseBotRuntime(argv)
		return err == nil && (parsed.Help || parsed.Self && !parsed.Offline && (parsed.Action == "list" || parsed.Action == "info"))
	}
	if argv[0] == "routine" {
		parsed, err := cliops.ParseRoutineRuntime(argv[1:])
		return err == nil && (parsed.Help || parsed.Self && parsed.Action == "list")
	}
	if argv[0] == "skill" {
		parsed, err := cliops.ParseSkillRuntime(argv[1:])
		return err == nil && (parsed.Help || parsed.Self && parsed.Action == "read")
	}
	if argv[0] != "mcp" {
		return false
	}
	parsed, err := cliops.ParseMCP(argv[1:])
	if err != nil || parsed.Offline || parsed.HTTP != "" || parsed.Bot != "" {
		return false
	}
	if parsed.Help {
		return true
	}
	if !parsed.Self {
		return false
	}
	switch parsed.Action {
	case "list", "tools", "describe":
		return true
	case "call":
		return parsed.ReadOnly
	}
	return false
}
