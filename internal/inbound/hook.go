package inbound

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
)

const (
	AuthHMAC   = "hmac"
	AuthBearer = "bearer"
)

// Hook is one webhook route. The secret stays in the bot directory, mode 0600.
type Hook struct {
	Name    string    `json:"name"`
	Secret  string    `json:"secret"`
	Auth    string    `json:"auth"`
	Created time.Time `json:"created"`
}

// AddHook writes a new secret and grants the name on the bot.
func AddHook(b *bot.Bot, name, auth string, now time.Time) (Hook, error) {
	if b == nil {
		return Hook{}, errors.New("webhook: nil bot")
	}
	if !ValidID(name) {
		return Hook{}, errors.New("webhook: invalid name")
	}
	auth = strings.ToLower(strings.TrimSpace(auth))
	if auth == "" {
		auth = AuthHMAC
	}
	if auth != AuthHMAC && auth != AuthBearer {
		return Hook{}, errors.New("webhook: auth must be hmac or bearer")
	}
	if _, err := LoadHook(b, name); err == nil {
		return Hook{}, errors.New("webhook: already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return Hook{}, err
	}
	secret, err := newSecret()
	if err != nil {
		return Hook{}, err
	}
	if now.IsZero() {
		now = time.Now()
	}
	h := Hook{Name: name, Secret: secret, Auth: auth, Created: now.UTC()}
	if err := saveHook(b, h); err != nil {
		return Hook{}, err
	}
	if err := b.GrantWebhook(name); err != nil {
		return Hook{}, err
	}
	return h, nil
}

// LoadHook reads one hook. A missing file is os.ErrNotExist.
func LoadHook(b *bot.Bot, name string) (Hook, error) {
	if !ValidID(name) {
		return Hook{}, errors.New("webhook: invalid name")
	}
	raw, err := os.ReadFile(hookPath(b, name))
	if err != nil {
		return Hook{}, err
	}
	var h Hook
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&h); err != nil {
		return Hook{}, err
	}
	if h.Secret == "" || (h.Auth != AuthHMAC && h.Auth != AuthBearer) {
		return Hook{}, errors.New("webhook: broken hook file")
	}
	return h, nil
}

// ListHooks returns granted hooks in config order. The secret is loaded for the caller; list output must not print it.
func ListHooks(b *bot.Bot) ([]Hook, error) {
	var out []Hook
	for _, name := range b.Config().Webhooks {
		h, err := LoadHook(b, name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, nil
}

// RemoveHook deletes the secret and the grant.
func RemoveHook(b *bot.Bot, name string) error {
	if !ValidID(name) {
		return errors.New("webhook: invalid name")
	}
	if err := os.Remove(hookPath(b, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return b.RevokeWebhook(name)
}

func saveHook(b *bot.Bot, h Hook) error {
	raw, err := fsutil.Marshal(h)
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(hookPath(b, h.Name), raw, 0600)
}

func hookPath(b *bot.Bot, name string) string {
	return filepath.Join(b.WebhookDir(), "id", name+".json")
}

func seenPath(b *bot.Bot) string {
	return filepath.Join(b.WebhookDir(), "seen.json")
}

func newSecret() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

// WebhookURL is the route a client posts to.
func WebhookURL(base, botName, hook string) string {
	return strings.TrimRight(base, "/") + "/webhook/" + botName + "/" + hook
}

func verifyHook(h Hook, body []byte, sigHeader, authHeader string) bool {
	switch h.Auth {
	case AuthHMAC:
		return verifyHMAC(h.Secret, body, sigHeader)
	case AuthBearer:
		return verifyBearer(h.Secret, authHeader)
	default:
		return false
	}
}

func verifyHMAC(secret string, body []byte, header string) bool {
	got, ok := parseSig(header)
	if !ok {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expect := mac.Sum(nil)
	return subtle.ConstantTimeCompare(got, expect) == 1
}

func parseSig(header string) ([]byte, bool) {
	i := strings.IndexByte(header, '=')
	if i <= 0 {
		return nil, false
	}
	if !strings.EqualFold(strings.TrimSpace(header[:i]), "sha256") {
		return nil, false
	}
	raw, err := hex.DecodeString(strings.TrimSpace(header[i+1:]))
	if err != nil || len(raw) != sha256.Size {
		return nil, false
	}
	return raw, true
}

func verifyBearer(secret, header string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	tok := strings.TrimSpace(header[len(prefix):])
	if len(tok) != len(secret) || secret == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(tok), []byte(secret)) == 1
}

// SignHMAC is the value of X-XBot-Signature for a body.
func SignHMAC(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

type seenFile struct {
	IDs []string `json:"ids"`
}

// rememberDelivery records a delivery id at accept time.
// A repeat is a duplicate even if the process restarts before the inbox post.
func rememberDelivery(b *bot.Bot, hook, delivery string) (bool, error) {
	if delivery == "" {
		return false, nil
	}
	key := hook + "\x00" + delivery
	dup := false
	err := fsutil.WithLock(filepath.Join(b.WebhookDir(), ".lock"), func() error {
		path := seenPath(b)
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
