// Package serve wakes bots whose chat session is not holding the handle lock.
// Due routines and file-watch events are always posted, even when the lock is
// held. Only the lock holder handles them, so one event is never run twice.
package serve

import (
	"context"
	"errors"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbound"
	"xbot/internal/kernel"
	"xbot/internal/proc"
)

// Pass posts due timer events for every bot, then handles a bot's inbox when
// this process can take the handle lock. A chat session that already holds the
// lock still receives the events in its own loop.
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
		k, err := newK(b)
		if err != nil {
			if k != nil {
				_ = k.Close()
			}
			return err
		}
		if _, err := kernel.FlushLegacyHeld(ctx, b, now, nil); err != nil && first == nil {
			first = err
		}
		if err := proc.DeliverDue(k, now); err != nil && first == nil {
			first = err
		}
		if err := inbound.DeliverFeeds(b, now, 0); err != nil && first == nil {
			first = err
		}
		lock, ok, err := b.TryLockHandle()
		if err != nil {
			_ = k.Close()
			return err
		}
		if !ok {
			_ = k.Close()
			continue
		}
		err = proc.Drain(ctx, k)
		_ = lock.Release()
		_ = k.Close()
		if err != nil && first == nil && !errors.Is(err, context.Canceled) {
			first = err
		}
	}
	return first
}
