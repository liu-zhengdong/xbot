// Package proc is the single wake loop for one bot.
// Delivery of due routines does not require the handle lock.
// Run holds the lock only while it handles one event, then releases it
// before waiting for the next line, so an idle chat does not block the web UI.
package proc

import (
	"context"
	"errors"
	"time"

	"xbot/internal/inbound"
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
		if cfg.K.Inbox != nil {
			if err := cfg.K.Inbox.Reload(); err != nil {
				return err
			}
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
			if cfg.K.Inbox != nil {
				if err := cfg.K.Inbox.Reload(); err != nil {
					_ = lock.Release()
					return err
				}
			}
			ev, ok = head(cfg.K)
			if !ok {
				_ = lock.Release()
				continue
			}
			handleErr := cfg.K.Handle(ctx, ev)
			if cfg.K.Inbox != nil && !errors.Is(handleErr, kernel.ErrPendingResult) {
				if err := cfg.K.Inbox.Ack(ev.ID); err != nil {
					_ = lock.Release()
					return err
				}
			}
			if cfg.After != nil {
				cfg.After(ev, handleErr)
			}
			_ = lock.Release()
			if handleErr != nil && errors.Is(handleErr, context.Canceled) {
				return nil
			}
			if errors.Is(handleErr, kernel.ErrPendingResult) {
				return handleErr
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
		// Best effort: a failed notice leaves the watermark for the next poll to
		// retry, and Expire waits so the retry still sees the missed one-shot.
		noticeErr := NoticeMissed(tm, k.Inbox.Post, now)
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
		// Expiring a missed one-shot is housekeeping; a bad routine file must
		// not stop the chat loop. The serve timer ticker retries every 15s.
		if noticeErr == nil {
			_ = tm.Expire(now)
		}
	}
	return nil
}

// Drain handles every pending event. The caller must hold the handle lock.
// A handle error is acked, then returned, except a result whose delivery is
// pending. That event must survive errors and restart until its reply commits.
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
		if errors.Is(handleErr, kernel.ErrPendingResult) {
			return handleErr
		}
		if err := k.Inbox.Ack(ev.ID); err != nil {
			return err
		}
		if handleErr != nil {
			return handleErr
		}
	}
}
