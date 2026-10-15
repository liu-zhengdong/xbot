package node

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"xbot/internal/fsutil"
	"xbot/internal/machine"
	"xbot/internal/tools"
)

// pendingID is np_ plus 16 hex chars. Anything else is not a file name we open.
var pendingID = regexp.MustCompile(`^np_[0-9a-f]{16}$`)

type pending struct {
	ID      string `json:"id"`
	Command string `json:"command"`
}

// RunShell executes command on this machine when the local switch is allow.
// approved is ignored. A remote caller cannot mark the command already confirmed.
// ask stores the command and returns an error that names xbot node approve.
func RunShell(root, command string, approved bool) (string, error) {
	_ = approved
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
		id, err := enqueue(root, command)
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("machine local is ask; xbot node approve %s", id)
	case machine.Allow:
	default:
		return "", fmt.Errorf("machine: bad mode %q", mode)
	}
	return runCommand(command)
}

// Approve runs a command that ask left on disk. The owner types this on the node.
// off still refuses, and a missing id does nothing.
func Approve(root, id string) (string, error) {
	mode, err := machine.Get(root, machine.Local)
	if err != nil {
		return "", err
	}
	if mode == machine.Off {
		return "", fmt.Errorf("machine local is off")
	}
	path, err := pendingPath(root, id)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("node: no pending %s", strings.TrimSpace(id))
		}
		return "", err
	}
	var item pending
	if err := json.Unmarshal(raw, &item); err != nil {
		return "", err
	}
	if strings.TrimSpace(item.Command) == "" {
		return "", fmt.Errorf("node: empty command")
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return runCommand(item.Command)
}

// LocalCall is the /v1/call handler for this machine. It does not trust Request.Approved.
func LocalCall(root string, info Info) func(Request) (string, error) {
	return func(req Request) (string, error) {
		switch strings.TrimSpace(req.Cap) {
		case "info":
			return InfoText(info), nil
		case "shell":
			return RunShell(root, req.Command, req.Approved)
		default:
			return "", fmt.Errorf("node: unknown cap %q", req.Cap)
		}
	}
}

func runCommand(command string) (string, error) {
	raw, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		return "", err
	}
	return tools.NewShell().Run(context.Background(), raw)
}

func enqueue(root, command string) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	id := "np_" + hex.EncodeToString(b[:])
	raw, err := json.Marshal(pending{ID: id, Command: command})
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, "node-pending", id+".json")
	if err := fsutil.WriteAtomic(path, append(raw, '\n'), 0600); err != nil {
		return "", err
	}
	return id, nil
}

func pendingPath(root, id string) (string, error) {
	id = strings.TrimSpace(id)
	if !pendingID.MatchString(id) {
		return "", fmt.Errorf("node: bad id")
	}
	return filepath.Join(root, "node-pending", id+".json"), nil
}
