package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
	"xbot/internal/gateway"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/tools"
)

type outboxItem struct {
	Env      gateway.Envelope `json:"env"`
	FromBot  string           `json:"from_bot,omitempty"`
	To       string           `json:"to,omitempty"`
	Snippet  string           `json:"snippet,omitempty"`
	Created  time.Time        `json:"created"`
	Attempts int              `json:"attempts"`
	Next     time.Time        `json:"next"`
}

func retryable(err error) bool {
	if err == nil {
		return false
	}
	var status *gateway.HTTPError
	if errors.As(err, &status) {
		return status.StatusCode >= 500 || status.StatusCode == 429 || status.StatusCode == 408 || status.StatusCode == 507
	}
	// Anything else from the client is transport or response trouble
	// (including body reads after the headers), so retry.
	return true
}

func enqueue(root string, item outboxItem) error {
	dir := filepath.Join(root, "remote", "outbox")
	return fsutil.WithLock(dir+".lock", func() error {
		raw, err := json.Marshal(item)
		if err != nil {
			return err
		}
		entries, err := os.ReadDir(dir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		count, size := 0, int64(len(raw))
		for _, entry := range entries {
			if entry.Name() == item.Env.ID+".json" || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			count++
			size += info.Size()
		}
		if count >= 256 || size > 8<<20 {
			return errors.New("remote: outbox full")
		}
		return fsutil.WriteAtomic(filepath.Join(dir, item.Env.ID+".json"), raw, 0600)
	})
}

// flushOutbox never holds the queue lock over network I/O.
func flushOutbox(ctx context.Context, root string, client *gateway.Client, now time.Time) error {
	dir := filepath.Join(root, "remote", "outbox")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	down := false
	for _, entry := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		var item outboxItem
		raw, err := os.ReadFile(path)
		if err != nil {
			continue // keep it; an I/O error is not corruption
		}
		if json.Unmarshal(raw, &item) != nil {
			// Malformed records must not block the rest of the queue.
			if rmErr := fsutil.WithLock(dir+".lock", func() error { return os.Remove(path) }); rmErr != nil {
				return rmErr
			}
			continue
		}
		expired := !now.Before(item.Created.Add(24 * time.Hour))
		if !expired && now.Before(item.Next) {
			continue
		}
		reason := "after 24h"
		if !expired {
			if down {
				continue // one pass stops sending after the first retryable failure; expiries still run
			}
			sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err = client.Send(sendCtx, item.Env)
			cancel()
			if retryable(err) {
				down = true
				item.Attempts++
				item.Next = now.Add(min(5*time.Second<<min(item.Attempts-1, 7), 10*time.Minute))
				if err := enqueue(root, item); err != nil {
					return err
				}
				continue
			}
			reason = "because the gateway rejected it"
		}
		if (expired || err != nil) && item.FromBot != "" && item.Env.Kind != "bounce" {
			text := fmt.Sprintf("message to %s was not delivered %s: %s", item.To, reason, item.Snippet)
			if err := postRemote(root, item.FromBot, "remote_undelivered_"+item.Env.ID, text, now); err != nil {
				return err
			}
		}
		if err := fsutil.WithLock(dir+".lock", func() error { return os.Remove(path) }); err != nil {
			return err
		}
	}
	return nil
}

func postRemote(root, to, id, text string, now time.Time) error {
	target, err := bot.Load(root, to)
	if errors.Is(err, bot.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	box, err := inbox.Open(target.InboxPath())
	if err != nil {
		return err
	}
	ev := link.NewEvent("remote", link.TypeMessage, tools.NeutralizeUntrusted(text), "remote", now)
	ev.ID = id
	return box.Post(ev)
}

func runOutbox(ctx context.Context, root string, logf func(string, ...any)) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		cfg, err := Load(root)
		if err == nil && cfg.Gateway != "" {
			priv, _, _, keyErr := Identity(root)
			err = keyErr
			if err == nil {
				client := &gateway.Client{Base: cfg.Gateway, Key: priv}
				tickPairing(ctx, root, client, time.Now())
				err = flushOutbox(ctx, root, client, time.Now())
			}
		}
		if err != nil && logf != nil && ctx.Err() == nil {
			logf("outbox local state failure")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
