package remote

import (
	"crypto/hmac"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
	"xbot/internal/gateway"
	"xbot/internal/inbox"
	"xbot/internal/link"
	"xbot/internal/peer"
	"xbot/internal/tools"
)

type bounce struct {
	Ref    string `json:"ref"`
	From   string `json:"from_bot"`
	To     string `json:"to_bot"`
	Reason string `json:"reason"`
}

// Receive acknowledges rejected envelopes too, but never operational failures.
func Receive(root, self string, envs []gateway.Envelope, now time.Time, logf ...func(string, ...any)) (ack []string, bounces []gateway.Envelope, err error) {
	path := filepath.Join(root, "remote", "seen.json")
	err = fsutil.WithLock(path+".lock", func() error {
		cfg, err := Load(root)
		if err != nil {
			return err
		}
		seen := map[string]int64{}
		if err := readJSON(path, &seen); err != nil {
			return err
		}
		if seen == nil {
			seen = map[string]int64{}
		}
		for id, ts := range seen {
			if ts < now.Add(-7*24*time.Hour).Unix() {
				delete(seen, id)
			}
		}
		var first error
		for _, env := range envs {
			if _, replay := seen[env.ID]; !replay {
				deliveryErr := deliver(root, self, cfg, env, now, &bounces, logf...)
				if deliveryErr == nil && (env.Kind == "pair" || env.Kind == "pake4" || env.Kind == "unpair") {
					cfg, deliveryErr = Load(root)
				}
				if err := deliveryErr; err != nil {
					if first == nil {
						first = err
					}
					continue
				}
			}
			seen[env.ID] = now.Unix()
			ack = append(ack, env.ID)
		}
		ids := make([]string, 0, len(seen))
		for id := range seen {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool {
			if seen[ids[i]] == seen[ids[j]] {
				return ids[i] < ids[j]
			}
			return seen[ids[i]] > seen[ids[j]]
		})
		for _, id := range ids[min(4096, len(ids)):] {
			delete(seen, id)
		}
		raw, err := json.Marshal(seen)
		if err != nil {
			ack, bounces = nil, nil
			return err
		}
		if len(raw) > 1<<20 {
			ack, bounces = nil, nil
			return errors.New("remote: seen state exceeds 1 MiB")
		}
		if err := fsutil.WriteAtomic(path, raw, 0600); err != nil {
			ack, bounces = nil, nil
			return err
		}
		return first
	})
	return
}

func deliver(root, self string, cfg Config, env gateway.Envelope, now time.Time, bounces *[]gateway.Envelope, logf ...func(string, ...any)) error {
	if env.Kind == "pake1" || env.Kind == "pake2" || env.Kind == "pake3" || env.Kind == "pake4" {
		return receiveShort(root, self, env, now)
	}
	if env.Kind == "pair" {
		return deliverPair(root, self, env, now, logf...)
	}
	d, paired := cfg.ByID(env.From)
	pub, err := base64.StdEncoding.DecodeString(d.Key)
	if env.To != self || !paired || err != nil || env.Verify(pub) != nil || env.V != 1 || (env.Kind != "box" && env.Kind != "bounce" && env.Kind != "dir" && env.Kind != "unpair" && env.Kind != "chat" && env.Kind != "chatreply") || env.Time.Before(now.Add(-7*24*time.Hour-5*time.Minute)) || env.Time.After(now.Add(5*time.Minute)) || len(env.Body) > gateway.MaxBody {
		return nil
	}
	priv, _, err := XIdentity(root)
	if err != nil {
		return err
	}
	body, err := openBox(priv, env)
	if err != nil {
		if len(logf) > 0 && logf[0] != nil {
			logf[0]("remote: cannot decrypt %s", env.ID)
		}
		return nil
	}
	if !d.UnpairedAt.IsZero() {
		return nil // It unpaired us: drop what it still sends until it is removed or paired again.
	}
	if env.Kind == "dir" {
		return receiveDirectory(root, d.ID, body, now) // The sidebar still needs the bot list.
	}
	if d.NoBotMessaging && (env.Kind == "box" || env.Kind == "bounce") {
		return nil // 「Bot 互发消息」 off: acked below and dropped, no inbox post, no bounce.
	}
	if env.Kind == "unpair" {
		return receiveUnpair(root, d.ID, body, now)
	}
	if env.Kind == "chat" {
		return receiveChat(root, d, env, body, now, bounces)
	}
	if env.Kind == "chatreply" {
		return receiveChatReply(root, d, env, body, now)
	}
	if env.Kind == "bounce" {
		var b bounce
		if json.Unmarshal(body, &b) != nil || !bot.ValidName(b.From) || !bot.ValidName(b.To) || b.Ref == "" || b.Reason != "no such bot" {
			return nil
		}
		target, err := bot.Load(root, b.From)
		if errors.Is(err, bot.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if d.Person == "" || !slices.Contains(target.Config().Owners, d.Person) {
			return nil
		}
		if err := (&peer.Bus{Root: root}).Allow("remote-in:"+d.Name+":"+b.To+"->"+b.From, now); err != nil {
			if errors.Is(err, peer.ErrRateLimit) {
				return nil
			}
			return err
		}
		return postRemote(root, b.From, "remote_bounce_"+env.ID, "message to "+b.To+"@"+d.Name+" was not delivered: no such bot", now)
	}
	var m message
	if json.Unmarshal(body, &m) != nil || !bot.ValidName(m.From) || !bot.ValidName(m.To) || m.Hops < 1 || m.Hops > peer.MaxHops {
		return nil
	}
	m.Text = strings.TrimSpace(m.Text)
	if m.Text == "" || len(m.Text) > peer.MaxText {
		return nil
	}
	if err := (&peer.Bus{Root: root}).Allow("remote-in:"+d.Name+":"+m.From+"->"+m.To, now); err != nil {
		if errors.Is(err, peer.ErrRateLimit) {
			return nil
		}
		return err
	}
	target, err := bot.Load(root, m.To)
	if err != nil && !errors.Is(err, bot.ErrNotFound) {
		return err
	}
	if errors.Is(err, bot.ErrNotFound) || d.Person == "" || !slices.Contains(target.Config().Owners, d.Person) {
		if d.XKey == "" {
			return nil
		}
		if err := (&peer.Bus{Root: root}).Allow("remote-bounce:"+d.ID, now); err != nil {
			if errors.Is(err, peer.ErrRateLimit) {
				return nil
			}
			return err
		}
		key, _, _, err := Identity(root)
		if err != nil {
			return err
		}
		body, err := json.Marshal(bounce{env.ID, m.From, m.To, "no such bot"})
		if err != nil {
			return err
		}
		reply, err := newBoxEnvelope(key, d, "bounce", body, now)
		if err == nil {
			*bounces = append(*bounces, reply)
		}
		return err
	}
	sender := m.From + "@" + d.Name
	ev := link.NewEvent("bot:"+sender, link.TypeMessage, tools.NeutralizeUntrusted(m.Text), sender, now)
	// A crash after Post but before persisting seen must not duplicate delivery.
	ev.ID = "remote_" + env.From + "_" + env.ID
	ev.SenderKind, ev.Hops, ev.Chain = link.KindBot, m.Hops, m.Chain
	box, err := inbox.Open(target.InboxPath())
	if err != nil {
		return err
	}
	return box.Post(ev)
}

func deliverPair(root, self string, env gateway.Envelope, now time.Time, logf ...func(string, ...any)) error {
	if env.To != self || env.V != 1 || len(env.Body) > maxPairBody ||
		env.Time.Before(now.Add(-7*24*time.Hour-5*time.Minute)) || env.Time.After(now.Add(5*time.Minute)) {
		return nil
	}
	cfg, err := Load(root)
	if err != nil {
		return err
	}
	var pr pairRequest
	if json.Unmarshal(env.Body, &pr) != nil || !namePattern.MatchString(pr.Name) || !isB64(pr.XKey, 32) {
		return nil
	}
	pub, err := base64.StdEncoding.DecodeString(pr.Key)
	if err != nil || len(pub) != 32 {
		return nil
	}
	mac, err := base64.StdEncoding.DecodeString(pr.Mac)
	if err != nil || len(mac) != 32 || gateway.ID(pub) != env.From || env.Verify(pub) != nil {
		return nil
	}
	known, paired := cfg.ByID(env.From)
	if paired && known.Key != pr.Key {
		return nil
	}
	blob := pairDomain + pr.Key + "|" + pr.XKey + "|" + pr.Name + "|" + self + "|" + env.ID
	return updateInvites(root, func(list []invite) ([]invite, error) {
		for i, iv := range list {
			if iv.Used || iv.Expires <= now.Unix() {
				continue
			}
			secret, err := base64.RawURLEncoding.DecodeString(iv.Secret)
			if err != nil || len(secret) != 32 || !hmac.Equal(hmacSHA256(secret, blob), mac) {
				continue
			}
			// Already paired with this key: a join retry, or a crash after
			// AddDevice; either way the invite is consumed now.
			if !paired {
				d, err := addDeviceUnique(root, pr.Name, pr.Key, pr.XKey, iv.Person)
				if err != nil {
					return nil, err
				}
				if len(logf) > 0 && logf[0] != nil {
					logf[0]("paired with %s %s", d.Name, d.ID[:8])
				}
			} else {
				if known.XKey == "" {
					if err := fillXKey(root, known.ID, pr.XKey); err != nil {
						return nil, err
					}
				}
				clearUnpaired(root, known.ID) // Paired again: it talks to us again.
			}
			list[i].Used = true
			break
		}
		return list, nil
	})
}

func addDeviceUnique(root, name, key, xkey, person string) (Device, error) {
	var d Device
	var err error
	for i := 1; i <= 9; i++ {
		n := name
		if i > 1 {
			n = fmt.Sprintf("%.62s-%d", name, i)
		}
		if d, err = AddDevice(root, n, key, xkey, person); err == nil {
			return d, nil
		}
	}
	return Device{}, err
}
