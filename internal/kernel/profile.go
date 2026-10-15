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

// ReleaseQuiet writes held notifications as one outbox note when now is outside quiet hours.
// desk may be nil; a nil desk uses the desktop notifier.
func ReleaseQuiet(ctx context.Context, b *bot.Bot, now time.Time, desk func(context.Context, string, string) error) (int, error) {
	if b == nil {
		return 0, nil
	}
	if b.InQuietAt(now) {
		return 0, nil
	}
	notes, err := notify.TakeHeld(b.HeldPath())
	if err != nil || len(notes) == 0 {
		return 0, err
	}
	text := notify.BatchBody(notes)
	if _, err := notify.Open(b.OutboxPath()).Append(text, now); err != nil {
		return 0, err
	}
	if desk == nil {
		desk = notify.Desktop
	}
	_ = desk(ctx, b.Name(), text)
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
	return "Propose a profile change: name, description, speaking style, proactivity, quiet hours, or timezone. It is saved only after the owner confirms, and applies on the next wake."
}
func (profileTool) Parameters() any {
	return schema(map[string]any{
		"name":        stringProp("new bot name; letters, digits, _ and -"),
		"description": stringProp("one-line description; empty clears it"),
		"tone":        stringProp("speaking tone; empty clears it"),
		"length":      stringProp("short, medium, long, or empty"),
		"emoji":       stringProp("none, spare, often, or empty"),
		"language":    stringProp("default reply language; empty clears it"),
		"proactivity": stringProp("quiet, normal, active, or empty"),
		"quiet_start": stringProp("quiet hours start HH:MM in the bot's timezone; set with quiet_end"),
		"quiet_end":   stringProp("quiet hours end HH:MM in the bot's timezone; set with quiet_start"),
		"timezone":    stringProp("IANA timezone such as Asia/Shanghai; empty uses the machine zone"),
	}, nil)
}
func (profileTool) Confirm(args json.RawMessage) (bool, string, error) {
	_, _, summary, err := parseProfileArgs(args)
	if err != nil {
		return false, "", err
	}
	return true, summary, nil
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
		"name", "description", "tone", "length", "emoji",
		"language", "proactivity", "quiet_start", "quiet_end", "timezone",
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
		case "tone":
			update.Tone = &s
			parts = append(parts, "tone "+quote(s))
		case "length":
			update.Length = &s
			parts = append(parts, "length "+quote(s))
		case "emoji":
			update.Emoji = &s
			parts = append(parts, "emoji "+quote(s))
		case "language":
			update.Language = &s
			parts = append(parts, "language "+quote(s))
		case "proactivity":
			update.Proactivity = &s
			parts = append(parts, "proactivity "+quote(s))
		case "quiet_start":
			update.QuietStart = &s
			parts = append(parts, "quiet_start "+quote(s))
		case "quiet_end":
			update.QuietEnd = &s
			parts = append(parts, "quiet_end "+quote(s))
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
