package kernel

import (
	"xbot/internal/bot"
	"xbot/internal/inbox"
)

func (k *Kernel) currentState() (st bot.State, err error) {
	err = k.withCurrentIO(func(b *bot.Bot, _ *inbox.Inbox) error { st, err = b.LoadState(); return err })
	return
}
func (k *Kernel) saveCurrentState(st bot.State) error {
	return k.withCurrentIO(func(b *bot.Bot, _ *inbox.Inbox) error { return b.SaveState(st) })
}
func (k *Kernel) appendCurrentEntry(entry bot.Entry) error {
	return k.withCurrentIO(func(b *bot.Bot, _ *inbox.Inbox) error { return b.Append(entry) })
}
func (k *Kernel) appendCurrentSeq(entry bot.Entry) (seq int, err error) {
	err = k.withCurrentIO(func(b *bot.Bot, _ *inbox.Inbox) error { seq, err = b.AppendSeq(entry); return err })
	return
}
func (k *Kernel) findCurrentEvent(id string) (seq int, err error) {
	err = k.withCurrentIO(func(b *bot.Bot, _ *inbox.Inbox) error { seq, err = b.FindEvent(id); return err })
	return
}
func (k *Kernel) currentCount() (count int, err error) {
	err = k.withCurrentIO(func(b *bot.Bot, _ *inbox.Inbox) error { count, err = b.Count(); return err })
	return
}
func (k *Kernel) currentSlice(from, to int) (entries []bot.Entry, err error) {
	err = k.withCurrentIO(func(b *bot.Bot, _ *inbox.Inbox) error { entries, err = b.Slice(from, to); return err })
	return
}

// The file read is short; consumers of the immutable entries run outside route.
func (k *Kernel) currentWalk(from, to int, fn func(int, bot.Entry) error) error {
	entries, err := k.currentSlice(from, to)
	if err != nil {
		return err
	}
	if from < 1 {
		from = 1
	}
	for i, entry := range entries {
		if err := fn(from+i, entry); err != nil {
			return err
		}
	}
	return nil
}
func (k *Kernel) appendCurrentFork(branch string, from int, task string) (seq int, err error) {
	err = k.withCurrentIO(func(b *bot.Bot, _ *inbox.Inbox) error { seq, err = b.AppendFork(branch, from, task); return err })
	return
}
func (k *Kernel) appendCurrentForkEnd(branch, status string, steps int) (seq int, err error) {
	err = k.withCurrentIO(func(b *bot.Bot, _ *inbox.Inbox) error { seq, err = b.AppendForkEnd(branch, status, steps); return err })
	return
}
