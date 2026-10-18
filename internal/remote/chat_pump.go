package remote

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/gateway"
	"xbot/internal/link"
)

// chatGiveUp is when a pending reply is dropped unsent.
const chatGiveUp = 24 * time.Hour

// pumpChatReplies sends each pending answer once its turn is done. Cheap when
// no reply is pending.
func pumpChatReplies(ctx context.Context, root string, client *gateway.Client, now time.Time) error {
	dir := filepath.Join(root, "remote", "chatpending")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var first error
	for _, entry := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		var p chatPending
		raw, err := os.ReadFile(path)
		if err != nil {
			first = errors.Join(first, err)
			continue
		}
		if json.Unmarshal(raw, &p) != nil || p.Event.ID == "" || !strings.HasSuffix(strings.TrimSuffix(entry.Name(), ".json"), p.Event.ID) {
			if err := os.Remove(path); err != nil {
				first = errors.Join(first, err)
			}
			continue
		}
		if !now.Before(p.At.Add(chatGiveUp)) {
			if err := os.Remove(path); err != nil {
				first = errors.Join(first, err)
			}
			continue
		}
		done, text, err := chatReplyReady(root, p)
		if err != nil {
			first = errors.Join(first, err)
			continue
		}
		if !done {
			continue
		}
		cfg, err := Load(root)
		if err != nil {
			first = errors.Join(first, err)
			continue
		}
		d, ok := cfg.ByID(p.Device)
		if !ok || !d.UnpairedAt.IsZero() {
			// The asking device is gone; its line can no longer be answered.
			if err := os.Remove(path); err != nil {
				first = errors.Join(first, err)
			}
			continue
		}
		var bounces []gateway.Envelope
		if err := queueChatReply(root, d, map[string]any{"ref": p.ID, "bot": p.Bot, "text": text, "at": now.Unix()}, now, &bounces); err != nil {
			first = errors.Join(first, err)
			continue
		}
		sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err = client.Send(sendCtx, bounces[0])
		cancel()
		if err != nil {
			first = errors.Join(first, err) // keep the file for the next cycle
			continue
		}
		if err := os.Remove(path); err != nil {
			first = errors.Join(first, err)
		}
	}
	return first
}

// chatReplyReady reports whether the turn that eventID started has finished
// and what its final answer is. A missing bot or a foreign event never replies.
func chatReplyReady(root string, p chatPending) (done bool, text string, err error) {
	target, err := bot.Load(root, p.Bot)
	if errors.Is(err, bot.ErrNotFound) {
		return true, "", nil
	}
	if err != nil {
		return false, "", err
	}
	if TurnReply == nil {
		return false, "", nil // Not wired (CLI process): serve answers it.
	}
	text, done, err = TurnReply(target, p.Event)
	return done, text, err
}

// TurnReply reads a finished turn's final answer from a bot's history; serve
// sets it to kernel.ReplyAfter (the kernel imports this package, so it cannot
// be called directly).
var TurnReply func(b *bot.Bot, ev link.Event) (text string, done bool, err error)
