package kernel

import (
	"context"
	"encoding/json"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/peer"
)

func (k *Kernel) isPrimary() bool {
	if k == nil || k.Bot == nil {
		return false
	}
	name, err := bot.Primary(k.Bot.Root)
	return err == nil && name == k.currentName()
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
	if name != t.k.currentName() {
		visible, err := peer.VisibleBots(t.k.Bot.Root, t.k.currentName())
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
	var err error
	if name == t.k.currentName() {
		err = t.k.withCurrentIO(func(current *bot.Bot, _ *inbox.Inbox) error {
			name = current.Name()
			return current.SetPinned(in.Pinned)
		})
	} else {
		var target *bot.Bot
		target, err = bot.Load(t.k.Bot.Root, name)
		if err == nil {
			err = target.SetPinned(in.Pinned)
		}
	}
	if err != nil {
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
	if name != t.k.currentName() {
		visible, err := peer.VisibleBots(t.k.Bot.Root, t.k.currentName())
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
