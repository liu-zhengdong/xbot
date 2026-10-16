package inbound

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/vault"
)

func (s *Server) handleConnector(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	botName := r.PathValue("bot")
	if (kind != link.NameTelegram && kind != link.NameLark) || !validBot(botName) {
		writeStatus(w, http.StatusNotFound, "not_found")
		return
	}
	b, err := bot.Load(s.Root, botName)
	if err != nil {
		if errors.Is(err, bot.ErrNotFound) {
			writeStatus(w, http.StatusNotFound, "not_found")
			return
		}
		writeStatus(w, http.StatusInternalServerError, "error")
		return
	}
	if !b.Grants(kind) {
		writeStatus(w, http.StatusNotFound, "not_found")
		return
	}
	secret, ok := s.connectorSecret(b, kind)
	if !ok {
		writeStatus(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	body, err := readBody(r.Body)
	if err != nil {
		if errors.Is(err, errTooBig) {
			writeStatus(w, http.StatusRequestEntityTooLarge, "too_large")
			return
		}
		writeStatus(w, http.StatusBadRequest, "bad_request")
		return
	}
	if !connectorAuth(kind, secret, body, r.Header) {
		writeStatus(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if kind == link.NameLark {
		if challenge, ok := larkChallenge(body); ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"challenge": challenge})
			return
		}
	}
	msg, ok := parseConnector(kind, body)
	if !ok {
		writeStatus(w, http.StatusOK, "ignored")
		return
	}
	dup, err := rememberConnector(b, kind, msg.delivery)
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "error")
		return
	}
	if dup {
		writeStatus(w, http.StatusOK, "duplicate")
		return
	}
	box, err := inbox.Open(b.InboxPath())
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "error")
		return
	}
	ev := link.NewEvent(kind, link.TypeMessage, msg.content, msg.sender, s.now())
	if err := box.Post(ev); err != nil {
		writeStatus(w, http.StatusInternalServerError, "error")
		return
	}
	writeStatus(w, http.StatusOK, "accepted")
}

// connectorSecret returns the value an inbound webhook must be verified
// against. Verification needs the plaintext, so it is only possible when this
// process holds the credential: a local vault store, or the legacy plaintext
// file before migration. A daemon-backed backend never hands the value out and
// the request is refused rather than accepted unverified.
func (s *Server) connectorSecret(b *bot.Bot, kind string) (string, bool) {
	if s.OpenVault != nil {
		backend, err := s.OpenVault(b)
		if err != nil {
			log.Printf("inbound: %s webhook for %s cannot be verified: opening its vault failed: %v", kind, b.Name(), err)
			return "", false
		}
		if backend == nil {
			return "", false
		}
		if local, ok := backend.(*vault.Store); ok {
			if sec, ok := local.Lookup(link.VaultName(kind)); ok && sec.Value != "" {
				return sec.Value, true
			}
			return "", false
		}
		// A vault daemon keeps the value outside this process.
		log.Printf("inbound: %s webhook for %s cannot be verified: the vault daemon does not expose the secret to this process", kind, b.Name())
		return "", false
	}
	vals, err := link.ReadSecrets(b.SecretPath())
	if err != nil {
		return "", false
	}
	secret := strings.TrimSpace(vals[link.SecretName(kind)])
	return secret, secret != ""
}

func connectorAuth(kind, secret string, body []byte, h http.Header) bool {
	var got string
	switch kind {
	case link.NameTelegram:
		got = h.Get("X-Telegram-Bot-Api-Secret-Token")
	case link.NameLark:
		got = strings.TrimSpace(h.Get("X-Lark-Token"))
		if got == "" {
			got = larkToken(body)
		}
	default:
		return false
	}
	return secretMatch(got, secret)
}

func secretMatch(got, want string) bool {
	if want == "" || len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

type connectorMsg struct {
	delivery string
	sender   string
	content  string
}

func parseConnector(kind string, body []byte) (connectorMsg, bool) {
	switch kind {
	case link.NameTelegram:
		return parseTelegram(body)
	case link.NameLark:
		return parseLark(body)
	default:
		return connectorMsg{}, false
	}
}

func parseTelegram(body []byte) (connectorMsg, bool) {
	var up struct {
		UpdateID int64      `json:"update_id"`
		Message  *tgMessage `json:"message"`
		Channel  *tgMessage `json:"channel_post"`
	}
	if json.Unmarshal(body, &up) != nil {
		return connectorMsg{}, false
	}
	msg := up.Message
	if msg == nil {
		msg = up.Channel
	}
	if msg == nil {
		return connectorMsg{}, false
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return connectorMsg{}, false
	}
	delivery := strconv.FormatInt(up.UpdateID, 10)
	if up.UpdateID == 0 {
		delivery = "m" + strconv.FormatInt(msg.MessageID, 10)
	}
	sender := "telegram"
	if msg.From != nil {
		if name := safeSender(msg.From.Username); name != "" {
			sender = name
		} else if msg.From.ID != 0 {
			sender = "tg" + strconv.FormatInt(msg.From.ID, 10)
		}
	}
	return connectorMsg{
		delivery: delivery,
		sender:   sender,
		content:  chatText(strconv.FormatInt(msg.Chat.ID, 10), text),
	}, true
}

type tgMessage struct {
	MessageID int64 `json:"message_id"`
	From      *struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	} `json:"from"`
	Chat struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	Text string `json:"text"`
}

func parseLark(body []byte) (connectorMsg, bool) {
	var up struct {
		Header struct {
			EventID string `json:"event_id"`
		} `json:"header"`
		Event struct {
			Sender struct {
				SenderID struct {
					OpenID string `json:"open_id"`
				} `json:"sender_id"`
			} `json:"sender"`
			Message struct {
				MessageID string `json:"message_id"`
				ChatID    string `json:"chat_id"`
				Content   string `json:"content"`
			} `json:"message"`
		} `json:"event"`
		Text   string `json:"text"`
		Sender string `json:"sender"`
		Chat   string `json:"chat"`
	}
	if json.Unmarshal(body, &up) != nil {
		return connectorMsg{}, false
	}
	text := strings.TrimSpace(up.Text)
	chat := strings.TrimSpace(up.Chat)
	sender := safeSender(up.Sender)
	delivery := strings.TrimSpace(up.Event.Message.MessageID)
	if delivery == "" {
		delivery = strings.TrimSpace(up.Header.EventID)
	}
	if text == "" && up.Event.Message.Content != "" {
		var content struct {
			Text string `json:"text"`
		}
		if json.Unmarshal([]byte(up.Event.Message.Content), &content) == nil {
			text = strings.TrimSpace(content.Text)
		}
		if chat == "" {
			chat = strings.TrimSpace(up.Event.Message.ChatID)
		}
		if sender == "" {
			sender = safeSender(up.Event.Sender.SenderID.OpenID)
		}
	}
	if text == "" {
		return connectorMsg{}, false
	}
	if sender == "" {
		sender = "lark"
	}
	return connectorMsg{delivery: delivery, sender: sender, content: chatText(chat, text)}, true
}

func larkChallenge(body []byte) (string, bool) {
	var up struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
	}
	if json.Unmarshal(body, &up) != nil || up.Type != "url_verification" {
		return "", false
	}
	if !challengeOK(up.Challenge) {
		return "", false
	}
	return up.Challenge, true
}

func larkToken(body []byte) string {
	var up struct {
		Token  string `json:"token"`
		Header struct {
			Token string `json:"token"`
		} `json:"header"`
	}
	if json.Unmarshal(body, &up) != nil {
		return ""
	}
	if strings.TrimSpace(up.Token) != "" {
		return strings.TrimSpace(up.Token)
	}
	return strings.TrimSpace(up.Header.Token)
}

func challengeOK(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.':
		default:
			return false
		}
	}
	return true
}

func chatText(chat, text string) string {
	if len(text) > 8<<10 {
		text = text[:8<<10]
	}
	if !utf8.ValidString(text) {
		text = strings.ToValidUTF8(text, "")
	}
	chat = strings.TrimSpace(chat)
	if chat == "" || chat == "0" {
		return text
	}
	return "chat: " + chat + "\n" + text
}

func safeSender(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 64 || !senderRe.MatchString(s) {
		return ""
	}
	return s
}

func rememberConnector(b *bot.Bot, kind, delivery string) (bool, error) {
	if delivery == "" {
		return false, nil
	}
	key := kind + "\x00" + delivery
	dir := filepath.Join(bot.Dir(b.Root, b.Name()), "connectors")
	path := filepath.Join(dir, "seen.json")
	dup := false
	err := fsutil.WithLock(filepath.Join(dir, ".lock"), func() error {
		var sf seenFile
		raw, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &sf); err != nil {
				return err
			}
		}
		for _, id := range sf.IDs {
			if id == key {
				dup = true
				return nil
			}
		}
		sf.IDs = append(sf.IDs, key)
		if len(sf.IDs) > MaxSeen {
			sf.IDs = sf.IDs[len(sf.IDs)-MaxSeen:]
		}
		raw, err = fsutil.Marshal(sf)
		if err != nil {
			return err
		}
		return fsutil.WriteAtomic(path, raw, 0600)
	})
	return dup, err
}
