package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/peer"
)

func (k *Kernel) isPrimary() bool {
	if k == nil || k.Bot == nil {
		return false
	}
	name, err := bot.Primary(k.Bot.Root)
	return err == nil && name == k.Bot.Name()
}

type createBotTool struct{ k *Kernel }

func (createBotTool) Name() string { return "create_bot" }
func (createBotTool) Description() string {
	return "Create a bot with an agreed purpose/persona and an empty transcript. Do not call when its purpose or duties are unclear: first use ask_user (or a plain question) to establish its name, what it will do, and whether to create it now. Ask only for missing information. A clear purpose/persona and name, or explicit permission to choose the name, allow direct creation. Choosing a name does not authorize inventing duties. For ask_user, recommend deferring creation while purpose is unknown, never invent a default persona on timeout. You remain the primary bot; person memory is already shared."
}
func (createBotTool) Parameters() any {
	return schema(map[string]any{
		"name":    stringProp("new bot name"),
		"persona": stringProp("non-empty duties/persona grounded in the owner's stated purpose"),
	}, []string{"name", "persona"})
}
func (createBotTool) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }
func (t createBotTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	if !t.k.isPrimary() {
		return "", errNotPrimary
	}
	var in struct {
		Name    string `json:"name"`
		Persona string `json:"persona"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	if strings.TrimSpace(in.Persona) == "" {
		return "", errors.New("create_bot: purpose/persona is required; ask the owner what this bot should do before creating it")
	}
	links := append([]string(nil), t.k.Bot.Config().Links...)
	b, err := bot.Create(t.k.Bot.Root, strings.TrimSpace(in.Name), strings.TrimSpace(in.Persona), links)
	if err != nil {
		return "", err
	}
	if owners := t.k.Bot.Config().Owners; len(owners) > 0 {
		if err := b.SetOwners(owners); err != nil {
			return "", err
		}
	}
	return "created " + b.Name(), nil
}

type deleteBotTool struct{ k *Kernel }

func (deleteBotTool) Name() string { return "delete_bot" }
func (deleteBotTool) Description() string {
	return "Delete another same-owner bot. The primary and the last bot cannot be deleted. Transfer the primary first."
}
func (deleteBotTool) Parameters() any {
	return schema(map[string]any{"name": stringProp("bot name")}, []string{"name"})
}
func (deleteBotTool) Confirm(args json.RawMessage) (bool, string, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return false, "", err
	}
	return false, "delete bot " + strings.TrimSpace(in.Name), nil
}
func (t deleteBotTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	if !t.k.isPrimary() {
		return "", errNotPrimary
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	name := strings.TrimSpace(in.Name)
	if name != t.k.Bot.Name() {
		visible, err := peer.VisibleBots(t.k.Bot.Root, t.k.Bot.Name())
		if err != nil {
			return "", err
		}
		ok := false
		for _, cfg := range visible {
			if cfg.Name == name {
				ok = true
				break
			}
		}
		if !ok {
			return "", errNotVisible
		}
	}
	if err := bot.Delete(t.k.Bot.Root, name); err != nil {
		return "", err
	}
	return "deleted " + name, nil
}

type pinBotTool struct{ k *Kernel }

func (pinBotTool) Name() string { return "pin_bot" }
func (pinBotTool) Description() string {
	return "Pin or unpin a same-owner bot. Pinned bots sort first. The primary starts pinned."
}
func (pinBotTool) Parameters() any {
	return schema(map[string]any{
		"name":   stringProp("bot name"),
		"pinned": boolProp("true to pin, false to unpin"),
	}, []string{"name", "pinned"})
}
func (pinBotTool) Confirm(json.RawMessage) (bool, string, error) { return false, "", nil }
func (t pinBotTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	if !t.k.isPrimary() {
		return "", errNotPrimary
	}
	var in struct {
		Name   string `json:"name"`
		Pinned bool   `json:"pinned"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	name := strings.TrimSpace(in.Name)
	if name != t.k.Bot.Name() {
		visible, err := peer.VisibleBots(t.k.Bot.Root, t.k.Bot.Name())
		if err != nil {
			return "", err
		}
		ok := false
		for _, cfg := range visible {
			if cfg.Name == name {
				ok = true
				break
			}
		}
		if !ok {
			return "", errNotVisible
		}
	}
	b, err := bot.Load(t.k.Bot.Root, name)
	if err != nil {
		return "", err
	}
	if err := b.SetPinned(in.Pinned); err != nil {
		return "", err
	}
	if in.Pinned {
		return "pinned " + name, nil
	}
	return "unpinned " + name, nil
}

type setPrimaryTool struct{ k *Kernel }

func (setPrimaryTool) Name() string { return "set_primary" }
func (setPrimaryTool) Description() string {
	return "Transfer the primary role to another same-owner bot. That bot becomes pinned and receives input that does not name a bot."
}
func (setPrimaryTool) Parameters() any {
	return schema(map[string]any{"name": stringProp("bot name")}, []string{"name"})
}
func (setPrimaryTool) Confirm(args json.RawMessage) (bool, string, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return false, "", err
	}
	return false, "make " + strings.TrimSpace(in.Name) + " the primary", nil
}
func (t setPrimaryTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	if !t.k.isPrimary() {
		return "", errNotPrimary
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	name := strings.TrimSpace(in.Name)
	if name != t.k.Bot.Name() {
		visible, err := peer.VisibleBots(t.k.Bot.Root, t.k.Bot.Name())
		if err != nil {
			return "", err
		}
		ok := false
		for _, cfg := range visible {
			if cfg.Name == name {
				ok = true
				break
			}
		}
		if !ok {
			return "", errNotVisible
		}
	}
	if err := bot.SetPrimary(t.k.Bot.Root, name); err != nil {
		return "", err
	}
	return "primary is " + name, nil
}

type primaryError struct{ msg string }

func (e primaryError) Error() string { return e.msg }

var (
	errNotPrimary = primaryError{"only the primary bot can do that"}
	errNotVisible = primaryError{"that bot is not visible to you"}
)

func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}
