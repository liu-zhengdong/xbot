package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"xbot/internal/link"
	"xbot/internal/tools"
)

// Link lists registered nodes and calls them. Schemas stay off the model list until load.
type Link struct {
	Root string
}

// Open binds the registry at root.
func Open(root string) *Link { return &Link{Root: root} }

func (l *Link) Name() string { return link.NameNode }

func (l *Link) Tools() []tools.Tool {
	if l == nil {
		return nil
	}
	return []tools.Tool{listTool{l}, callTool{l}}
}

type listTool struct{ l *Link }

func (listTool) Name() string { return "node_list" }
func (listTool) Description() string {
	return "List nodes the owner registered with xbot node add, and whether each one answers."
}
func (listTool) Parameters() any {
	return map[string]any{
		"type": "object", "properties": map[string]any{},
		"additionalProperties": false,
	}
}
func (listTool) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }

func (t listTool) Run(ctx context.Context, _ json.RawMessage) (string, error) {
	nodes, err := List(t.l.Root)
	if err != nil {
		return "", err
	}
	if len(nodes) == 0 {
		return "no nodes", nil
	}
	var b strings.Builder
	for _, n := range nodes {
		state := "offline"
		detail := ""
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		info, err := Ping(pingCtx, n.URL, n.Token)
		cancel()
		switch {
		case err == nil:
			state = "online"
			detail = info.Name
			if len(info.Caps) > 0 {
				detail += " caps: " + strings.Join(info.Caps, ",")
			}
		case errors.Is(err, ErrUnauthorized):
			state = "unauthorized"
		}
		fmt.Fprintf(&b, "%s\t%s\t%s\n", n.Name, state, detail)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

type callTool struct{ l *Link }

func (callTool) Name() string { return "node_call" }
func (callTool) Description() string {
	return "Call a capability on a registered node. info returns the machine id, name, OS, and arch. shell runs a command there when that machine's switch is allow. ask does not run it; someone on that machine must run xbot node approve. A call cannot mark itself approved."
}
func (callTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":    map[string]any{"type": "string", "description": "Registered node name"},
			"cap":     map[string]any{"type": "string", "description": "info or shell"},
			"command": map[string]any{"type": "string", "description": "Shell command when cap is shell"},
		},
		"required":             []string{"name", "cap"},
		"additionalProperties": false,
	}
}
func (callTool) Confirm(args json.RawMessage) (bool, string, error) {
	var in struct {
		Name    string `json:"name"`
		Cap     string `json:"cap"`
		Command string `json:"command"`
	}
	if len(args) > 0 && string(args) != "null" {
		if err := json.Unmarshal(args, &in); err != nil {
			return false, "", err
		}
	}
	if strings.TrimSpace(in.Cap) == "shell" {
		return true, "node " + strings.TrimSpace(in.Name) + " shell\n" + in.Command, nil
	}
	return false, "", nil
}

func (t callTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Name    string `json:"name"`
		Cap     string `json:"cap"`
		Command string `json:"command"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	n, err := Lookup(t.l.Root, strings.TrimSpace(in.Name))
	if err != nil {
		return "", err
	}
	cap := strings.TrimSpace(in.Cap)
	if cap == "" {
		return "", fmt.Errorf("node: cap is required")
	}
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return Call(callCtx, n.URL, n.Token, cap, in.Command, false)
}
