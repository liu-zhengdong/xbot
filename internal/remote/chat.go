package remote

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
	"xbot/internal/gateway"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/peer"
)

// G7e batch A covers one person's own devices. A message counts as the owner's
// own words only when all three hold:
//  1. it arrives as an authenticated kind chat envelope from a paired device
//     (the checks in deliver) and decrypted body says "typed": true;
//  2. the device is bound to a person who is in the target bot's Owners;
//  3. that person sent it from a web owner session through SendOwnerChat.
// Anything else of kind chat is acked and dropped. Bot relays stay on kind box.

// MaxChatText is the longest trimmed chat line, in bytes.
const MaxChatText = 16 << 10

type chatPending struct {
	Device string     `json:"device"`
	Bot    string     `json:"bot"`
	ID     string     `json:"id"`
	Event  link.Event `json:"event"`
	At     time.Time  `json:"at"`
}

func pendingPath(root, evID string) string {
	return filepath.Join(root, "remote", "chatpending", evID+".json")
}

// receiveChat handles kind chat on the receiving computer. It returns quickly
// after the inbox post and queues one ack reply for the caller to send.
func receiveChat(root string, d Device, env gateway.Envelope, body []byte, now time.Time, bounces *[]gateway.Envelope) error {
	var c struct {
		ID    string `json:"id"`
		Bot   string `json:"bot"`
		Text  string `json:"text"`
		Typed bool   `json:"typed"`
		At    int64  `json:"at"`
	}
	if json.Unmarshal(body, &c) != nil || !c.Typed || !bot.ValidName(c.Bot) || len(c.ID) != 16 {
		return nil
	}
	if _, err := hex.DecodeString(c.ID); err != nil {
		return nil
	}
	text := strings.TrimSpace(c.Text)
	if text == "" || len(text) > MaxChatText {
		return nil
	}
	target, err := bot.Load(root, c.Bot)
	if errors.Is(err, bot.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if d.Person == "" || !slices.Contains(target.Config().Owners, d.Person) {
		return nil
	}
	if err := (&peer.Bus{Root: root}).Allow("remote-chat:"+d.Name+":"+c.Bot, now); err != nil {
		if errors.Is(err, peer.ErrRateLimit) {
			return nil
		}
		return err
	}
	ev := link.RemoteOwnerEvent(d.Person, text, now)
	if !link.OwnerTrusted(ev.Source, ev.Sender, target.Config().Owners) {
		return nil
	}
	// A crash after Post but before seen keeps the inbox free of duplicates.
	ev.ID = "rchat_" + env.From + "_" + c.ID
	p := chatPending{Device: env.From, Bot: c.Bot, ID: c.ID, Event: ev, At: now}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	// The pending file comes first: a crash before the post then leaves a file
	// whose reply waits for a turn that never starts, and it expires.
	if err := fsutil.WriteAtomic(pendingPath(root, ev.ID), raw, 0600); err != nil {
		return err
	}
	box, err := inbox.Open(target.InboxPath())
	if err != nil {
		return err
	}
	if err := box.Post(ev); err != nil {
		return err
	}
	return queueChatReply(root, d, map[string]any{"ref": c.ID, "bot": c.Bot, "ack": true}, now, bounces)
}

// queueChatReply seals one kind chatreply to d into bounces. The caller sends it.
func queueChatReply(root string, d Device, body map[string]any, now time.Time, bounces *[]gateway.Envelope) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	key, _, _, err := Identity(root)
	if err != nil {
		return err
	}
	reply, err := newBoxEnvelope(key, d, "chatreply", raw, now)
	if err != nil {
		return err
	}
	*bounces = append(*bounces, reply)
	return nil
}
