// Package proc is the single wake loop for one bot.
// Delivery of due routines does not require the handle lock.
// Handling does: the process that holds the lock is the only consumer.
package proc

import (
	"context"
	"errors"
	"time"

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

// Config drives one processor. The caller holds the bot's handle lock.
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
	// After runs after each handled event.
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
		if cfg.K.Inbox != nil {
			if err := cfg.K.Inbox.Reload(); err != nil {
				return err
			}
		}
		if !cfg.SkipDeliver {
			if err := DeliverDue(cfg.K, now()); err != nil {
				return err
			}
		}
		if ev, ok := head(cfg.K); ok {
			idle = false
			handleErr := cfg.K.Handle(ctx, ev)
			if cfg.K.Inbox != nil {
				if err := cfg.K.Inbox.Ack(ev.ID); err != nil {
					return err
				}
			}
			if cfg.After != nil {
				cfg.After(ev, handleErr)
			}
			if handleErr != nil && errors.Is(handleErr, context.Canceled) {
				return nil
			}
			continue
		}
		if !idle {
			idle = true
			if cfg.Idle != nil {
				cfg.Idle()
			}
		}
		var lines <-chan Line
		if cfg.Lines != nil && cfg.Accept != nil {
			lines = cfg.Lines
		}
		var wake <-chan struct{}
		if cfg.K.Inbox != nil {
			wake = cfg.K.Inbox.Wait()
		}
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

func head(k *kernel.Kernel) (link.Event, bool) {
	if k.Inbox == nil {
		return link.Event{}, false
	}
	return k.Inbox.Head()
}

// DeliverDue posts timer events that are due. It does not handle them.
// A repeated slot is one event id, so a second poster does not double-wake.
func DeliverDue(k *kernel.Kernel, now time.Time) error {
	if k == nil || k.Inbox == nil {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	for _, l := range k.Links {
		tm, ok := l.(*link.Timer)
		if !ok || tm == nil {
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
	return nil
}

// Drain handles every pending event. The caller must hold the handle lock.
// A handle error is acked, then returned, so a poison event is not retried forever.
func Drain(ctx context.Context, k *kernel.Kernel) error {
	if k == nil || k.Inbox == nil {
		return nil
	}
	if err := k.Inbox.Reload(); err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
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
