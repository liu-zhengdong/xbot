package kernel

import (
	"xbot/internal/bot"
)

func (k *Kernel) isPrimary() bool {
	if k == nil || k.Bot == nil {
		return false
	}
	name, err := bot.Primary(k.Bot.Root)
	return err == nil && name == k.currentName()
}

type primaryError struct{ msg string }

func (e primaryError) Error() string { return e.msg }

var (
	errNotPrimary = primaryError{"only the primary bot can do that"}
	errNotVisible = primaryError{"that bot is not visible to you"}
)
