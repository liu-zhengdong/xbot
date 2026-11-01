package kernel

import (
	"context"
	"errors"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/notify"
	"xbot/internal/peer"
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
	current, moved, err := bot.RenameWithResult(root, from, to)
	if moved {
		err = errors.Join(err, peer.RenameMember(root, from, current.Name()))
	}
	return current, moved, err
}

// RenameRuntime is the name-only entry into the typed runtime profile domain.
// The host callback encloses the existing resource movers.
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
