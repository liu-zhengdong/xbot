package remote

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
)

// maxChatFile caps one transcript; the oldest lines go when it is exceeded.
const maxChatFile = 2 << 20

// ChatMessage is one transcript line on the computer that started the chat.
type ChatMessage struct {
	ID    string    `json:"id"`
	Role  string    `json:"role"` // "me" (owner) or "bot"
	Text  string    `json:"text"`
	At    time.Time `json:"at"`
	State string    `json:"state"` // queued_local, sent, delivered, answered, failed
	Ref   string    `json:"ref,omitempty"`
}

func chatDir(root string) string { return filepath.Join(root, "remote", "chats") }

func chatPath(root, device, name string) string {
	return filepath.Join(chatDir(root), device, name+".jsonl")
}

func newChatID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// SendOwnerChat sends a line the owner typed here to a bot on another paired
// device. Only the web owner session calls it: the body says "typed": true,
// which is what the other computer trusts. A network, 5xx, or 429 failure puts
// the sealed envelope in the outbox, so the returned state is queued_local.
func SendOwnerChat(ctx context.Context, root, deviceID, botName, text string, now time.Time) (ChatMessage, error) {
	if !bot.ValidName(botName) {
		return ChatMessage{}, errors.New("remote: invalid bot name")
	}
	text = strings.TrimSpace(text)
	if text == "" || len(text) > MaxChatText {
		return ChatMessage{}, errors.New("remote: chat line must be 1..16 KiB")
	}
	cfg, err := Load(root)
	if err != nil {
		return ChatMessage{}, err
	}
	d, ok := cfg.ByID(deviceID)
	if !ok {
		return ChatMessage{}, errors.New("remote: paired device required")
	}
	if !d.UnpairedAt.IsZero() {
		return ChatMessage{}, fmt.Errorf("remote: device %s was unpaired by the other computer", d.Name)
	}
	if d.XKey == "" {
		return ChatMessage{}, fmt.Errorf("remote: %s has no encryption key; pair again with xbot remote invite/join", d.Name)
	}
	id, err := newChatID()
	if err != nil {
		return ChatMessage{}, err
	}
	body, err := json.Marshal(map[string]any{"id": id, "bot": botName, "text": text, "typed": true, "at": now.Unix()})
	if err != nil {
		return ChatMessage{}, err
	}
	priv, _, _, err := Identity(root)
	if err != nil {
		return ChatMessage{}, err
	}
	env, err := newBoxEnvelope(priv, d, "chat", body, now)
	if err != nil {
		return ChatMessage{}, err
	}
	// The line is stored before the envelope leaves, so an early ack or reply
	// always finds it.
	m := ChatMessage{ID: id, Role: "me", Text: text, At: now.UTC(), State: "queued_local"}
	if err := appendChat(root, d.ID, botName, m); err != nil {
		return ChatMessage{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	sendErr := (&gateway.Client{Base: cfg.GatewayURL(), Key: priv}).Send(ctx, env)
	cancel()
	switch {
	case retryable(sendErr):
		if err := enqueue(root, outboxItem{Env: env, To: botName + "@" + d.Name, Created: now, Attempts: 1, Next: now.Add(5 * time.Second)}); err != nil {
			m.State = "failed"
			sendErr = err
		}
	case sendErr != nil:
		m.State = "failed"
	default:
		m.State = "sent"
	}
	if m.State != "queued_local" {
		err := updateChat(root, d.ID, botName, func(lines []ChatMessage) ([]ChatMessage, bool) {
			for i := range lines {
				// An ack that already arrived (delivered, answered) wins.
				if lines[i].ID == id && lines[i].Role == "me" && lines[i].State == "queued_local" {
					lines[i].State = m.State
					return lines, true
				}
			}
			return lines, false
		})
		if err != nil {
			return m, err
		}
	}
	return m, sendErr
}

// ChatHistory returns the stored transcript oldest first, at most limit lines.
func ChatHistory(root, deviceID, botName string, limit int) ([]ChatMessage, error) {
	cfg, err := Load(root)
	if err != nil {
		return nil, err
	}
	d, ok := cfg.ByID(deviceID)
	if !ok {
		return nil, errors.New("remote: paired device required")
	}
	if !bot.ValidName(botName) {
		return nil, errors.New("remote: invalid bot name")
	}
	lines, err := readChat(root, d.ID, botName)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines, nil
}

// appendChat adds one line under the file lock and keeps the newest lines
// when the file passes maxChatFile.
func appendChat(root, device, name string, m ChatMessage) error {
	path := chatPath(root, device, name)
	return fsutil.WithLock(path+".lock", func() error {
		if err := fsutil.AppendJSONL(path, m); err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil || info.Size() <= maxChatFile {
			return err
		}
		lines, err := readChat(root, device, name)
		if err != nil {
			return err
		}
		return writeChat(path, lines)
	})
}

// updateChat reads, changes, and rewrites one transcript under its lock, so a
// concurrent append is never lost.
func updateChat(root, device, name string, change func([]ChatMessage) ([]ChatMessage, bool)) error {
	path := chatPath(root, device, name)
	return fsutil.WithLock(path+".lock", func() error {
		lines, err := readChat(root, device, name)
		if err != nil {
			return err
		}
		lines, changed := change(lines)
		if !changed {
			return nil
		}
		return writeChat(path, lines)
	})
}

// writeChat writes the newest lines that fit in maxChatFile. The caller holds the lock.
func writeChat(path string, lines []ChatMessage) error {
	var buf []byte
	for i := len(lines) - 1; i >= 0; i-- {
		raw, err := json.Marshal(lines[i])
		if err != nil {
			return err
		}
		if len(buf)+len(raw)+1 > maxChatFile && len(buf) > 0 {
			break
		}
		buf = append(append(raw, '\n'), buf...)
	}
	return fsutil.WriteAtomic(path, buf, 0600)
}

func readChat(root, device, name string) ([]ChatMessage, error) {
	var out []ChatMessage
	err := fsutil.ReadJSONL(chatPath(root, device, name), func(line []byte) error {
		var m ChatMessage
		if json.Unmarshal(line, &m) != nil || m.ID == "" || (m.Role != "me" && m.Role != "bot") {
			return nil // a damaged line never blocks the rest
		}
		out = append(out, m)
		return nil
	})
	return out, err
}

// receiveChatReply handles kind chatreply on the asking computer. A reply that
// names no line of that device's transcript is dropped, so the device cannot
// start a chat line here.
func receiveChatReply(root string, d Device, env gateway.Envelope, body []byte, now time.Time) error {
	var r struct {
		Ref  string `json:"ref"`
		Bot  string `json:"bot"`
		Text string `json:"text"`
		Ack  bool   `json:"ack"`
		At   int64  `json:"at"`
	}
	if json.Unmarshal(body, &r) != nil || !bot.ValidName(r.Bot) || len(r.Ref) != 16 {
		return nil
	}
	if _, err := hex.DecodeString(r.Ref); err != nil {
		return nil
	}
	text := strings.TrimSpace(r.Text)
	if !r.Ack && (text == "" || len(text) > MaxChatText) {
		return nil
	}
	at := time.Unix(r.At, 0)
	if r.At <= 0 || at.Before(now.Add(-7*24*time.Hour)) || at.After(now.Add(5*time.Minute)) {
		at = now
	}
	return updateChat(root, d.ID, r.Bot, func(lines []ChatMessage) ([]ChatMessage, bool) {
		i := -1
		for j, m := range lines {
			if m.ID == r.Ref && m.Role == "me" {
				i = j
			}
		}
		if i < 0 {
			return lines, false
		}
		cur := lines[i].State
		if r.Ack {
			if cur != "queued_local" && cur != "sent" && cur != "failed" {
				return lines, false
			}
			lines[i].State = "delivered"
			return lines, true
		}
		if cur == "answered" {
			return lines, false
		}
		lines[i].State = "answered"
		return append(lines, ChatMessage{ID: env.ID, Role: "bot", Text: text, At: at.UTC(), State: "answered", Ref: r.Ref}), true
	})
}
