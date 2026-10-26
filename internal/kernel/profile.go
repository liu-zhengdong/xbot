package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/notify"
	"xbot/internal/peer"
	"xbot/internal/tools"
)

// FlushLegacyHeld delivers notes that quiet hours left in held.json before
// quiet hours were removed, each as an ordinary note, and removes the file.
// desk may be nil; a nil desk uses the desktop notifier.
func FlushLegacyHeld(ctx context.Context, b *bot.Bot, now time.Time, desk func(context.Context, string, string) error) (int, error) {
	if b == nil {
		return 0, nil
	}
	if desk == nil {
		desk = notify.Desktop
	}
	notes, err := notify.TakeLegacyHeld(b.LegacyHeldPath())
	if err != nil || len(notes) == 0 {
		return 0, err
	}
	out := notify.Open(b.OutboxPath())
	for _, text := range notes {
		if text = strings.TrimSpace(text); text == "" {
			continue
		}
		if _, err := out.Append(text, now); err != nil {
			return 0, err
		}
		_ = desk(ctx, b.Name(), text)
	}
	return len(notes), nil
}

// RenameBot moves the bot directory and rewrites group membership.
// The primary file is updated when it named this bot. Pin state stays as it was.
func RenameBot(root, from, to string) error {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == to {
		return nil
	}
	if err := peer.RenameMember(root, from, to); err != nil {
		return err
	}
	if err := bot.Rename(root, from, to); err != nil {
		_ = peer.RenameMember(root, to, from)
		return err
	}
	return nil
}

type profileTool struct{ k *Kernel }

func (profileTool) Name() string { return "propose_profile" }
func (profileTool) Description() string {
	return "Propose a profile change: name, description, or timezone. It is saved only after the owner confirms, and applies on the next wake."
}
func (profileTool) Parameters() any {
	return schema(map[string]any{
		"name":        stringProp("new bot name; letters, digits, _ and -"),
		"description": stringProp("one-line description; empty clears it"),
		"timezone":    stringProp("IANA timezone such as Asia/Shanghai; empty uses the machine zone"),
	}, nil)
}
func (profileTool) Confirm(args json.RawMessage) (bool, string, error) {
	_, _, summary, err := parseProfileArgs(args)
	if err != nil {
		return false, "", err
	}
	return false, summary, nil
}
func (t profileTool) Run(_ context.Context, args json.RawMessage) (string, error) {
	name, update, _, err := parseProfileArgs(args)
	if err != nil {
		return "", err
	}
	if update != (bot.ProfileUpdate{}) {
		if err := t.k.Bot.ApplyProfile(update); err != nil {
			return "", err
		}
	}
	if name != nil {
		next := strings.TrimSpace(*name)
		if next != "" && next != t.k.Bot.Name() {
			old := t.k.Bot.Name()
			if err := RenameBot(t.k.Bot.Root, old, next); err != nil {
				return "", err
			}
			loaded, err := bot.Load(t.k.Bot.Root, next)
			if err != nil {
				return "", err
			}
			t.k.Bot = loaded
		}
	}
	return "profile saved; it applies on the next wake", nil
}

func parseProfileArgs(args json.RawMessage) (*string, bot.ProfileUpdate, string, error) {
	if len(args) == 0 {
		return nil, bot.ProfileUpdate{}, "", errors.New("profile: nothing to change")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(args, &raw); err != nil {
		return nil, bot.ProfileUpdate{}, "", err
	}
	if len(raw) == 0 {
		return nil, bot.ProfileUpdate{}, "", errors.New("profile: nothing to change")
	}
	known := []string{
		"name", "description", "timezone",
	}
	allowed := map[string]bool{}
	for _, key := range known {
		allowed[key] = true
	}
	for key := range raw {
		if !allowed[key] {
			return nil, bot.ProfileUpdate{}, "", fmt.Errorf("profile: unknown field %q", key)
		}
	}
	var update bot.ProfileUpdate
	var name *string
	var parts []string
	for _, key := range known {
		val, ok := raw[key]
		if !ok {
			continue
		}
		var s string
		if err := json.Unmarshal(val, &s); err != nil {
			return nil, bot.ProfileUpdate{}, "", fmt.Errorf("profile: %s must be a string", key)
		}
		switch key {
		case "name":
			name = &s
			parts = append(parts, "name "+quote(s))
		case "description":
			update.Description = &s
			parts = append(parts, "description "+quote(s))
		case "timezone":
			update.Timezone = &s
			parts = append(parts, "timezone "+quote(s))
		}
	}
	if name == nil && update == (bot.ProfileUpdate{}) {
		return nil, bot.ProfileUpdate{}, "", errors.New("profile: nothing to change")
	}
	return name, update, "update profile: " + strings.Join(parts, ", "), nil
}

func quote(s string) string {
	if s == "" {
		return "(clear)"
	}
	return `"` + s + `"`
}

// Ensure profileTool implements tools.Tool.
var _ tools.Tool = profileTool{}
