package remote

import (
	"encoding/base64"
	"encoding/json"
	"errors"
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

// Receive acknowledges rejected envelopes too, but never operational failures.
func Receive(root, self string, envs []gateway.Envelope, now time.Time) (ack []string, err error) {
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
				if err := deliver(root, self, cfg, env, now); err != nil {
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
			ack = nil
			return err
		}
		if len(raw) > 1<<20 {
			ack = nil
			return errors.New("remote: seen state exceeds 1 MiB")
		}
		if err := fsutil.WriteAtomic(path, raw, 0600); err != nil {
			ack = nil
			return err
		}
		return first
	})
	return
}

func deliver(root, self string, cfg Config, env gateway.Envelope, now time.Time) error {
	d, paired := cfg.ByID(env.From)
	pub, err := base64.StdEncoding.DecodeString(d.Key)
	if env.To != self || !paired || err != nil || env.Verify(pub) != nil || env.V != 1 || env.Kind != "msg" || env.Time.Before(now.Add(-7*24*time.Hour-5*time.Minute)) || env.Time.After(now.Add(5*time.Minute)) || len(env.Body) > 64<<10 {
		return nil
	}
	var m message
	if json.Unmarshal(env.Body, &m) != nil || !bot.ValidName(m.From) || m.Hops < 1 || m.Hops > peer.MaxHops {
		return nil
	}
	m.Text = strings.TrimSpace(m.Text)
	if m.Text == "" || len(m.Text) > peer.MaxText {
		return nil
	}
	target, err := bot.Load(root, m.To)
	if errors.Is(err, bot.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if d.Person == "" || !slices.Contains(target.Config().Owners, d.Person) {
		return nil
	}
	if err := (&peer.Bus{Root: root}).Allow("remote-in:"+d.Name+":"+m.From+"->"+m.To, now); err != nil {
		if errors.Is(err, peer.ErrRateLimit) {
			return nil
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
