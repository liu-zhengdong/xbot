package remote

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"xbot/internal/fsutil"
	"xbot/internal/gateway"
	"xbot/internal/person"
)

const (
	inviteTTL   = 10 * time.Minute
	inviteMax   = 16
	codeMax     = 2 << 10
	pairPrefix  = "xbot-pair:"
	pairDomain  = "xbot-pair-v1|"
	maxPairBody = 4 << 10
)

var nonName = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

type invite struct {
	Secret  string `json:"secret"`
	Person  string `json:"person,omitempty"`
	Expires int64  `json:"expires"`
	Used    bool   `json:"used,omitempty"`
}

// PairCode carries the gateway credentials and one-time pairing secret.
type PairCode struct {
	V       int    `json:"v"`
	Gateway string `json:"g"`
	Token   string `json:"t"`
	Key     string `json:"k"`
	XKey    string `json:"x"`
	Name    string `json:"n"`
	Secret  string `json:"s"`
	Expires int64  `json:"e"`
}

type pairRequest struct {
	Name string `json:"name"`
	Key  string `json:"key"`
	XKey string `json:"xkey"`
	Mac  string `json:"mac"`
}

// updateInvites holds the lock across matching and consuming an invite.
func updateInvites(root string, fn func([]invite) ([]invite, error)) error {
	path := filepath.Join(root, "remote", "invites.json")
	return fsutil.WithLock(path+".lock", func() error {
		var list []invite
		err := readJSON(path, &list)
		if err != nil {
			return err
		}
		if list, err = fn(list); err != nil {
			return err
		}
		raw, _ := json.Marshal(list)
		return fsutil.WriteAtomic(path, raw, 0600)
	})
}

// hasLiveInvite reads atomically written state without changing or consuming it.
func hasLiveInvite(root string, now time.Time) (bool, error) {
	var list []invite
	if err := readJSON(filepath.Join(root, "remote", "invites.json"), &list); err != nil {
		return false, err
	}
	for _, iv := range list {
		if !iv.Used && iv.Expires > now.Unix() {
			return true, nil
		}
	}
	return false, nil
}

// CreateInvite mints one single-use, 10-minute code for a device to join.
func CreateInvite(root, personID string, now time.Time) (string, error) {
	cfg, err := Load(root)
	if err != nil {
		return "", err
	}
	if personID == "" {
		people, err := person.List(root)
		if err != nil {
			return "", err
		}
		if len(people) != 1 {
			return "", errors.New("remote: specify --person ID")
		}
		personID = people[0].ID
	}
	if _, err := person.Load(root, personID); err != nil {
		return "", err
	}
	_, _, keyB64, err := Identity(root)
	if err != nil {
		return "", err
	}
	_, xkeyB64, err := XIdentity(root)
	if err != nil {
		return "", err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	enc, expires := base64.RawURLEncoding.EncodeToString(secret), now.Add(inviteTTL).Unix()
	raw, _ := json.Marshal(PairCode{1, cfg.GatewayURL(), os.Getenv("XBOT_GATEWAY_TOKEN"), keyB64, xkeyB64, suggestName(), enc, expires})
	code := pairPrefix + base64.RawURLEncoding.EncodeToString(raw)
	if len(code) > codeMax {
		return "", errors.New("remote: invite code exceeds 2 KiB")
	}
	return code, updateInvites(root, func(list []invite) ([]invite, error) {
		live := list[:0]
		for _, iv := range list {
			if iv.Expires > now.Unix() {
				live = append(live, iv)
			}
		}
		live = append(live, invite{Secret: enc, Person: personID, Expires: expires})
		return live[max(0, len(live)-inviteMax):], nil
	})
}

func isB64(s string, n int) bool {
	b, err := base64.StdEncoding.DecodeString(s)
	return err == nil && len(b) == n
}

// ParseCode decodes and validates an invite code without touching the network.
func ParseCode(code string) (p PairCode, err error) {
	bad := errors.New("remote: invalid invite code")
	enc, ok := strings.CutPrefix(strings.TrimSpace(code), pairPrefix)
	if !ok || len(code) > codeMax {
		return PairCode{}, bad
	}
	raw, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return PairCode{}, bad
	}
	if json.Unmarshal(raw, &p) != nil || p.V != 1 || !isB64(p.Key, 32) || !isB64(p.XKey, 32) || !namePattern.MatchString(p.Name) {
		return PairCode{}, bad
	}
	secret, err := base64.RawURLEncoding.DecodeString(p.Secret)
	if err != nil || len(secret) != 32 {
		return PairCode{}, bad
	}
	u, err := url.Parse(p.Gateway)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return PairCode{}, bad
	}
	p.Gateway = strings.TrimRight(p.Gateway, "/")
	return p, nil
}

// Join records the inviter and sends one signed pairing reply. as is the
// name the inviter will use for this device (default: hostname).
func Join(ctx context.Context, root, code, name, as, personID string, now time.Time) (Device, error) {
	pc, err := ParseCode(code)
	if err != nil {
		return Device{}, err
	}
	if as == "" {
		as = suggestName()
	} else if !namePattern.MatchString(as) {
		return Device{}, fmt.Errorf("remote: bad device name %q", as)
	}
	if now.Unix() >= pc.Expires {
		return Device{}, errors.New("remote: invite expired")
	}
	priv, _, keyB64, err := Identity(root)
	if err != nil {
		return Device{}, err
	}
	_, xkeyB64, err := XIdentity(root)
	if err != nil {
		return Device{}, err
	}
	err = update(root, func(cfg *Config) error {
		// An install already paired on the default gateway must not silently move elsewhere.
		if (cfg.Gateway != "" || len(cfg.Devices) > 0) && cfg.GatewayURL() != pc.Gateway {
			return errors.New("remote: gateway mismatch")
		}
		if cfg.Gateway != "" || pc.Gateway != DefaultGateway {
			cfg.Gateway = pc.Gateway
		}
		return nil
	})
	if err != nil {
		return Device{}, err
	}
	client := &gateway.Client{Base: pc.Gateway, Key: priv}
	if err := client.Register(ctx, pc.Token); err != nil {
		return Device{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = pc.Name
	}
	cfg, err := Load(root)
	if err != nil {
		return Device{}, err
	}
	pub, _ := base64.StdEncoding.DecodeString(pc.Key)
	d, paired := cfg.ByID(gateway.ID(pub))
	if paired && d.Key == pc.Key && d.XKey == "" {
		err, d.XKey = fillXKey(root, d.ID, pc.XKey), pc.XKey
	} else if !paired || d.Key != pc.Key || d.XKey != pc.XKey {
		d, err = AddDevice(root, name, pc.Key, pc.XKey, personID)
	}
	if err != nil {
		return Device{}, err
	}
	env := gateway.NewEnvelope(priv, d.ID, "pair", nil, now)
	body := pairRequest{Name: as, Key: keyB64, XKey: xkeyB64}
	secret, _ := base64.RawURLEncoding.DecodeString(pc.Secret)
	body.Mac = base64.StdEncoding.EncodeToString(hmacSHA256(secret,
		pairDomain+body.Key+"|"+body.XKey+"|"+body.Name+"|"+d.ID+"|"+env.ID))
	env.Body, _ = json.Marshal(body)
	env.Sign(priv)
	return d, client.Send(ctx, env)
}

func hmacSHA256(key []byte, msg string) []byte {
	m := hmac.New(sha256.New, key)
	_, _ = m.Write([]byte(msg))
	return m.Sum(nil)
}

func suggestName() string {
	host, _ := os.Hostname()
	out := strings.Trim(nonName.ReplaceAllString(host, "-"), "-_")
	out = out[:min(64, len(out))]
	if !namePattern.MatchString(out) {
		return "xbot"
	}
	return out
}
