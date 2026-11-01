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
	_, _, err := renameBotResult(root, strings.TrimSpace(from), strings.TrimSpace(to))
	return err
}

func renameBotResult(root, from, to string) (*bot.Bot, bool, error) {
	if from == to {
		b, err := bot.Load(root, from)
		return b, false, err
	}
	if err := peer.RenameMember(root, from, to); err != nil {
		return nil, false, err
	}
	current, moved, err := bot.RenameWithResult(root, from, to)
	if err != nil && !moved {
		_ = peer.RenameMember(root, to, from)
	}
	return current, moved, err
}

type profileTool struct{ k *Kernel }

func (profileTool) Name() string { return "bot_update_profile" }
func (profileTool) Description() string {
	return "Update this bot's avatar/profile: name, description, timezone, or generated avatar via avatar_seed. To change avatars (换一批), set a new seed, conventionally 名字#n (e.g. XBot#3), matching Edit Profile. No image uploads. Changes are saved directly; the sidebar and header avatar refresh immediately in the current conversation."
}
func (profileTool) Parameters() any {
	return schema(map[string]any{
		"name":        stringProp("new bot name; letters, digits, _ and -"),
		"description": stringProp("one-line description; empty clears it"),
		"timezone":    stringProp("IANA timezone such as Asia/Shanghai; empty uses the machine zone"),
		"avatar_seed": stringProp("generated avatar seed for this bot; set a new seed for 换一批, conventionally 名字#n (e.g. XBot#3); 1–80 characters after trimming, no markup, quotes, ampersands, backticks or control characters; replaces any uploaded avatar"),
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
	t.k.operations.mu.Lock()
	defer t.k.operations.mu.Unlock()
	name, update, _, err := parseProfileArgs(args)
	if err != nil {
		return "", err
	}
	// A different source may already have renamed these shared resources.
	// Keep short profile IO on the current route, releasing it before the host.
	if r := t.k.Resources; r != nil {
		r.route.RLock()
		if r.closed || r.ctx.Err() != nil {
			r.route.RUnlock()
			return "", errors.New("bot resources closed")
		}
		t.k.Bot, t.k.Inbox = r.current, r.box
		if update != (bot.ProfileUpdate{}) {
			err = r.current.ApplyProfile(update)
		}
		r.route.RUnlock()
	} else if update != (bot.ProfileUpdate{}) {
		err = t.k.Bot.ApplyProfile(update)
	}
	if err != nil {
		return "", err
	}
	if name != nil {
		next := strings.TrimSpace(*name)
		if next != "" && next != t.k.Bot.Name() {
			if err := t.k.renameBot(next); err != nil {
				return "", err
			}
		}
	}
	if update.AvatarSeed != nil {
		return "profile saved; 头像已换好；侧栏与顶栏当面刷新", nil
	}
	return "profile saved", nil
}

// RenameRuntime is the web entry into the same source operation used by the
// legacy profile tool. The host callback encloses both existing movers.
func (k *Kernel) RenameRuntime(next string) error {
	change, err := bot.ValidateProfileChange(bot.ProfileChange{Name: &next})
	if err != nil {
		return err
	}
	_, err = k.ApplyProfileChange(change)
	return err
}

// renameBot is called with this source's operations lock already held.
func (k *Kernel) renameBot(next string) error {
	change, err := bot.ValidateProfileChange(bot.ProfileChange{Name: &next})
	if err != nil {
		return err
	}
	_, err = k.changeProfileLocked(change)
	return err
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
		"name", "description", "timezone", "avatar_seed",
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
		case "avatar_seed":
			if !bot.ValidAvatarSeed(s) {
				return nil, bot.ProfileUpdate{}, "", errors.New("profile: invalid avatar_seed")
			}
			s = strings.TrimSpace(s)
			update.AvatarSeed = &s
			parts = append(parts, "avatar_seed "+quote(s))
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
