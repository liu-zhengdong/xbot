// Package proc is the single wake loop for one bot.
// Delivery of due routines does not require the handle lock.
// Run holds the lock only while it handles one event, then releases it
// before waiting for the next line, so an idle chat does not block the web UI.
package proc

import (
	"context"
	"errors"
	"time"

	"xbot/internal/bot"
	"xbot/internal/inbound"
	"xbot/internal/inbox"
	"xbot/internal/kernel"
	"xbot/internal/link"
)

// ErrStop ends the loop without treating it as a failure. Chat uses it for quit.
var ErrStop = errors.New("proc: stop")

// Line is one stdin read.
type Line struct {
	Text string
	Err  error
}

// Config drives one processor. Run takes the handle lock around each event.
type Config struct {
	K    *kernel.Kernel
	Poll time.Duration
	Now  func() time.Time
	// SkipDeliver leaves routine delivery to another process (serve).
	// The loop still reloads the inbox and handles what it finds.
	SkipDeliver bool
	Lines       <-chan Line
	// Accept turns a user line into an inbox event. A nil event is ignored.
	// ErrStop ends the loop cleanly.
	Accept func(text string) (link.Event, error)
	// Idle runs once each time the inbox is empty and the loop is about to wait.
	Idle func()
	// CatchUp prints transcript lines another process appended.
	// It returns true when it printed something, so the prompt can be drawn again.
	CatchUp func() bool
	// After runs after each handled event, still holding the handle lock.
	After func(ev link.Event, err error)
}

// Run handles inbox events as they arrive and accepts user lines between them.
// Cancelled context ends the loop without an error.
func Run(ctx context.Context, cfg Config) error {
	if cfg.K == nil {
		return errors.New("proc: nil kernel")
	}
	poll := cfg.Poll
	if poll <= 0 {
		poll = 200 * time.Millisecond
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	tick := time.NewTicker(poll)
	defer tick.Stop()
	idle := false
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if err := reloadInbox(cfg.K); err != nil {
			return err
		}
		if !cfg.SkipDeliver {
			if err := DeliverDue(cfg.K, now()); err != nil {
				return err
			}
			if err := inbound.DeliverFeeds(cfg.K.Bot, now(), 0); err != nil {
				return err
			}
		}
		if ev, ok := head(cfg.K); ok {
			idle = false
			if cfg.K.Bot == nil {
				return errors.New("proc: nil bot")
			}
			lock, err := cfg.K.Bot.LockHandle()
			if err != nil {
				return err
			}
			if err := reloadInbox(cfg.K); err != nil {
				_ = lock.Release()
				return err
			}
			ev, ok = head(cfg.K)
			if !ok {
				_ = lock.Release()
				continue
			}
			handleErr := cfg.K.Handle(ctx, ev)
			if err := ackInbox(cfg.K, ev.ID); err != nil {
				_ = lock.Release()
				return err
			}
			if cfg.After != nil {
				cfg.After(ev, handleErr)
			}
			_ = lock.Release()
			if handleErr != nil && errors.Is(handleErr, context.Canceled) {
				return nil
			}
			continue
		}
		if !idle {
			idle = true
			if cfg.CatchUp != nil {
				cfg.CatchUp()
			}
			if cfg.Idle != nil {
				cfg.Idle()
			}
		} else if cfg.CatchUp != nil && cfg.CatchUp() && cfg.Idle != nil {
			cfg.Idle()
		}
		var lines <-chan Line
		if cfg.Lines != nil && cfg.Accept != nil {
			lines = cfg.Lines
		}
		var wake <-chan struct{}
		_ = withInboxIO(cfg.K, func(box *inbox.Inbox) error { wake = box.Wait(); return nil })
		select {
		case <-ctx.Done():
			return nil
		case <-wake:
		case <-tick.C:
		case line, ok := <-lines:
			if !ok {
				return nil
			}
			idle = false
			if line.Err != nil {
				return nil
			}
			if _, err := cfg.Accept(line.Text); err != nil {
				if errors.Is(err, ErrStop) {
					return nil
				}
				return err
			}
		}
	}
}

// Select the current Inbox for each short operation. No Handle, delivery
// callback, or channel wait runs under the resource route.
func withInboxIO(k *kernel.Kernel, fn func(*inbox.Inbox) error) error {
	if k.Resources != nil {
		return k.Resources.WithCurrentIO(func(_ *bot.Bot, box *inbox.Inbox) error { return fn(box) })
	}
	if k.Inbox == nil {
		return nil
	}
	return fn(k.Inbox)
}

func reloadInbox(k *kernel.Kernel) error {
	return withInboxIO(k, func(box *inbox.Inbox) error { return box.Reload() })
}

func ackInbox(k *kernel.Kernel, id string) error {
	return withInboxIO(k, func(box *inbox.Inbox) error { return box.Ack(id) })
}

func head(k *kernel.Kernel) (ev link.Event, ok bool) {
	_ = withInboxIO(k, func(box *inbox.Inbox) error { ev, ok = box.Head(); return nil })
	return
}

// DeliverDue posts timer events that are due. It does not handle them.
// A repeated slot is one event id, so a second poster does not double-wake.
func DeliverDue(k *kernel.Kernel, now time.Time) error {
	if k == nil || (k.Resources == nil && k.Inbox == nil) {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	post := func(ev link.Event) error {
		return withInboxIO(k, func(box *inbox.Inbox) error { return box.Post(ev) })
	}
	for _, l := range k.Links {
		tm, ok := l.(*link.Timer)
		if !ok || tm == nil {
			continue
		}
		// Best effort: a failed notice leaves the watermark for the next poll to
		// retry, and Expire waits so the retry still sees the missed one-shot.
		noticeErr := NoticeMissed(tm, post, now)
		evs, err := tm.Due(now)
		if err != nil {
			return err
		}
		for _, ev := range evs {
			if err := tm.Deliver(context.Background(), ev, post); err != nil {
				return err
			}
		}
		// Expiring a missed one-shot is housekeeping; a bad routine file must
		// not stop the chat loop. The serve timer ticker retries every 15s.
		if noticeErr == nil {
			_ = tm.Expire(now)
		}
	}
	return nil
}

// Drain handles every pending event. The caller must hold the handle lock.
// A handle error is acked, then returned, so a poison event is not retried forever.
func Drain(ctx context.Context, k *kernel.Kernel) error {
	if k == nil || (k.Resources == nil && k.Inbox == nil) {
		return nil
	}
	if err := reloadInbox(k); err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		ev, ok := head(k)
		if !ok {
			return nil
		}
		handleErr := k.Handle(ctx, ev)
		if err := ackInbox(k, ev.ID); err != nil {
			return err
		}
		if handleErr != nil {
			return handleErr
		}
	}
}
