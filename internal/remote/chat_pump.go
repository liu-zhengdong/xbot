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
	"xbot/internal/clip"
	"xbot/internal/gateway"
	"xbot/internal/link"
)

// chatGiveUp is when a pending reply is dropped unsent.
const chatGiveUp = 24 * time.Hour

// chatCutNote marks a reply the sender shortened. It is visible to the owner on
// the asking device, so a long answer never looks complete when it was cut.
const chatCutNote = "\n\n[已截断：这条回复超过 16 KiB，后面没发出的部分请在对话里查看]"

// clipChatText keeps text within MaxChatText bytes without splitting a rune.
// The asking computer rejects anything longer, so an unclipped long reply would
// be dropped there while this side believed it was delivered.
func clipChatText(text string) string {
	if len(text) <= MaxChatText {
		return text
	}
	cut, _ := clip.Bytes(text, MaxChatText-len(chatCutNote))
	return cut + chatCutNote
}

// pendingChatReplies reports whether any reply file is still waiting. The poll
// loop uses it to keep the wait short while an answer is owed, so a reply
// produced during the wait is not stuck behind a full-length poll.
func pendingChatReplies(root string) bool {
	dir := filepath.Join(root, "remote", "chatpending")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			return true
		}
	}
	return false
}

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
		// The asking computer drops a reply longer than MaxChatText and still acks
		// the envelope, so the answer would vanish silently. Cut it here and say so,
		// where the sender still knows the text was cut.
		text = clipChatText(text)
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
