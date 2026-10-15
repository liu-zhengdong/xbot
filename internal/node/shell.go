package node

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"xbot/internal/machine"
	"xbot/internal/tools"
)

// RunShell executes command on this machine when the local switch allows it.
// ask requires approved, which the caller's owner already confirmed.
func RunShell(root, command string, approved bool) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", fmt.Errorf("node: empty command")
	}
	mode, err := machine.Get(root, machine.Local)
	if err != nil {
		return "", err
	}
	switch mode {
	case machine.Off:
		return "", fmt.Errorf("machine local is off")
	case machine.Ask:
		if !approved {
			return "", fmt.Errorf("machine local is ask")
		}
	case machine.Allow:
	default:
		return "", fmt.Errorf("machine: bad mode %q", mode)
	}
	raw, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		return "", err
	}
	return tools.NewShell().Run(context.Background(), raw)
}
