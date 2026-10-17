// Package serve wakes bots whose chat session is not holding the handle lock.
// Due routines and file-watch events are always posted, even when the lock is
// held. Only the lock holder handles them, so one event is never run twice.
package serve

import (
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/proc"
)

// TimerEvery is how often DeliverTimers checks routines. It is shorter than
// the bot pass because a one-shot must not wait for every bot's model calls.
const TimerEvery = 15 * time.Second

// DeliverTimers posts due routine events for every bot and expires one-shots
// whose window was missed. It does not run a model: it only touches inboxes,
// so it is safe to run on its own ticker while a bot pass is draining. Posting
// is idempotent per event id and Commit only records the slot once.
func DeliverTimers(root string, now time.Time) error {
	if now.IsZero() {
		now = time.Now()
	}
	cfgs, err := bot.List(root)
	if err != nil {
		return err
	}
	var first error
	for _, cfg := range cfgs {
		b, err := bot.Load(root, cfg.Name)
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		if !hasTimer(b) {
			continue
		}
		if err := deliverBotTimers(b, now); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func hasTimer(b *bot.Bot) bool {
	for _, name := range b.Config().Links {
		if name == link.NameTimer {
			return true
		}
	}
	return false
}

func deliverBotTimers(b *bot.Bot, now time.Time) (err error) {
	sender := "timer"
	if owners := b.Config().Owners; len(owners) > 0 {
		sender = owners[0]
	}
	tm := &link.Timer{Dir: b.RoutineDir(), Sender: sender}
	now = now.UTC()
	noticeErr := proc.NoticeMissed(tm, func(ev link.Event) error {
		in, err := inbox.Open(b.InboxPath())
		if err != nil {
			return err
		}
		return in.Post(ev)
	}, now)
	// Notice failures must not block routine delivery, but Expire waits so the
	// retry still lists a missed one-shot.
	expire := func(now time.Time) error {
		if noticeErr != nil {
			return nil
		}
		return tm.Expire(now)
	}
	defer func() {
		if err == nil {
			err = noticeErr
		}
	}()
	evs, err := tm.Due(now)
	if err != nil {
		return err
	}
	if len(evs) == 0 {
		return expire(now)
	}
	in, err := inbox.Open(b.InboxPath())
	if err != nil {
		return err
	}
	for _, ev := range evs {
		if err := in.Post(ev); err != nil {
			return err
		}
		if err := tm.Commit(ev); err != nil {
			return err
		}
	}
	return expire(now)
}
