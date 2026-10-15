// Package serve runs bots when nobody is chatting: timers fire and inboxes drain.
package serve

import (
	"context"
	"errors"
	"time"

	"xbot/internal/bot"
	"xbot/internal/kernel"
	"xbot/internal/link"
)

// Pass visits each bot once. A bot whose chat session holds the handle lock is skipped.
func Pass(ctx context.Context, root string, now time.Time, newK func(*bot.Bot) (*kernel.Kernel, error)) error {
	if now.IsZero() {
		now = time.Now()
	}
	cfgs, err := bot.List(root)
	if err != nil {
		return err
	}
	var first error
	for _, cfg := range cfgs {
		if err := ctx.Err(); err != nil {
			return err
		}
		b, err := bot.Load(root, cfg.Name)
		if err != nil {
			return err
		}
		lock, ok, err := b.TryLockHandle()
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		err = passBot(ctx, b, now, newK)
		_ = lock.Release()
		if err != nil && first == nil && !errors.Is(err, context.Canceled) {
			first = err
		}
	}
	return first
}

func passBot(ctx context.Context, b *bot.Bot, now time.Time, newK func(*bot.Bot) (*kernel.Kernel, error)) error {
	k, err := newK(b)
	if err != nil {
		return err
	}
	if k.Inbox != nil {
		if err := k.Inbox.Reload(); err != nil {
			return err
		}
	}
	for _, l := range k.Links {
		tm, ok := l.(*link.Timer)
		if !ok || tm == nil || k.Inbox == nil {
			continue
		}
		evs, err := tm.Due(now)
		if err != nil {
			return err
		}
		for _, ev := range evs {
			if err := k.Inbox.Post(ev); err != nil {
				return err
			}
			if err := tm.Commit(ev); err != nil {
				return err
			}
		}
	}
	if k.Inbox == nil {
		return nil
	}
	for {
		ev, ok := k.Inbox.Head()
		if !ok {
			return nil
		}
		handleErr := k.Handle(ctx, ev)
		if err := k.Inbox.Ack(ev.ID); err != nil {
			return err
		}
		if handleErr != nil {
			return handleErr
		}
	}
}
