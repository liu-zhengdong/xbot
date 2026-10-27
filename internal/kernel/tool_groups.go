package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"xbot/internal/provider"
	"xbot/internal/tools"
)

// ToolGroup describes discovery, not authority. Members still come from the
// mounted directory and pass through the ordinary review wrappers when called.
type ToolGroup struct {
	ID, Name, Description string
	Tools                 []tools.Tool
}

func toolGroups(mounted []tools.Tool) []ToolGroup {
	g := ToolGroup{
		ID: "bot_profile", Name: "Bot资料编辑",
		Description: "修改本 Bot 的名字、简介、时区、头像或职责。先调用 expand_tool_group(group: bot_profile) 展开完整工具定义。",
	}
	for _, t := range mounted {
		switch t.Name() {
		case "bot_update_profile", "bot_propose_persona":
			g.Tools = append(g.Tools, t)
		}
	}
	if len(g.Tools) == 0 {
		return nil
	}
	return []ToolGroup{g}
}

func (k *Kernel) discloseToolGroups(mounted []tools.Tool) []tools.Tool {
	groups := toolGroups(mounted)
	hidden := map[string]bool{}
	list := make([]tools.Tool, 0, len(mounted)+len(groups)+1)
	k.loadMu.Lock()
	for _, g := range groups {
		if k.expandedToolGroups[g.ID] {
			continue
		}
		for _, t := range g.Tools {
			hidden[t.Name()] = true
		}
		list = append(list, toolGroupEntry{g})
	}
	k.loadMu.Unlock()
	for _, t := range mounted {
		if !hidden[t.Name()] {
			list = append(list, t)
		}
	}
	return append(list, expandToolGroup{k})
}

// The entry carries only a summary and an empty argument object for function
// calling protocols. It cannot perform edits or implicitly expand the group.
type toolGroupEntry struct{ group ToolGroup }

func (t toolGroupEntry) Name() string                                { return t.group.ID }
func (t toolGroupEntry) Description() string                         { return t.group.Name + "：" + t.group.Description }
func (toolGroupEntry) Parameters() any                               { return schema(nil, nil) }
func (toolGroupEntry) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }
func (t toolGroupEntry) Run(context.Context, json.RawMessage) (string, error) {
	return "", fmt.Errorf("tool group %s is not an action; call expand_tool_group with group %q first", t.group.ID, t.group.ID)
}

type expandToolGroup struct{ k *Kernel }

func (expandToolGroup) Name() string { return "expand_tool_group" }
func (expandToolGroup) Description() string {
	return "Expand a tool group by id or display name. Returns its full schemas and makes all its tools callable from the next step, across wakes of this live Bot kernel; restarting the kernel folds groups again."
}
func (expandToolGroup) Parameters() any {
	return schema(map[string]any{"group": stringProp("tool group id or display name")}, []string{"group"})
}
func (expandToolGroup) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }
func (t expandToolGroup) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Group string `json:"group"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	id := strings.TrimSpace(in.Group)
	if id == "" {
		return "", fmt.Errorf("expand_tool_group: group is required")
	}
	mounted, err := t.k.mountedTools()
	if err != nil {
		return "", err
	}
	for _, g := range toolGroups(mounted) {
		if id != g.ID && id != g.Name {
			continue
		}
		defs := make([]provider.ToolDef, 0, len(g.Tools))
		for _, tool := range g.Tools {
			defs = append(defs, provider.ToolDef{Name: tool.Name(), Description: tool.Description(), Parameters: tool.Parameters()})
		}
		out, err := json.Marshal(struct {
			Group string             `json:"group"`
			Tools []provider.ToolDef `json:"tools"`
		}{g.ID, defs})
		if err != nil {
			return "", err
		}
		// Publish only after the complete definitions have been built. No
		// transcript mutation: Env Stream samples modelTools on the next step.
		t.k.loadMu.Lock()
		if t.k.expandedToolGroups == nil {
			t.k.expandedToolGroups = map[string]bool{}
		}
		t.k.expandedToolGroups[g.ID] = true
		t.k.loadMu.Unlock()
		return string(out), nil
	}
	return "", fmt.Errorf("expand_tool_group: unknown or unavailable group %q", id)
}
