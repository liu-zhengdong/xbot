package remote

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
	"xbot/internal/gateway"
	"xbot/internal/peer"
)

// dirBot is one bot on the sending device. Only the portrait seed travels;
// an uploaded image stays on its own device.
type dirBot struct {
	Name   string `json:"name"`
	Seed   string `json:"seed,omitempty"`
	Custom bool   `json:"custom,omitempty"`
}
type directory struct {
	Bots []dirBot `json:"bots"`
	At   int64    `json:"at"`
}
type dirCache struct {
	directory
	Received time.Time `json:"received"`
}
type dirSent struct {
	Hash string `json:"hash"`
	At   int64  `json:"at"`
}

func directoryFor(root string, d Device) ([]dirBot, error) {
	list := make([]dirBot, 0)
	if d.Person == "" {
		return list, nil
	}
	bots, err := peer.OwnerBots(root, d.Person)
	if err != nil {
		return nil, err
	}
	for _, b := range bots[:min(64, len(bots))] {
		seed := b.AvatarSeed
		if seed == "" {
			seed = b.Name
		}
		list = append(list, dirBot{Name: b.Name, Seed: seed, Custom: b.Avatar == bot.AvatarCustom})
	}
	return list, nil
}

// dirRefresh resends an unchanged directory, so a peer that re-paired or lost
// its cache sees the list again within minutes; one small envelope per device.
const dirRefresh = 15 * time.Minute

func directoryDue(bots []dirBot, last dirSent, now time.Time) (dirSent, bool) {
	raw, _ := json.Marshal(bots)
	next := dirSent{fmt.Sprintf("%x", sha256.Sum256(raw)), now.Unix()}
	return next, next.Hash != last.Hash || !now.Before(time.Unix(last.At, 0).Add(dirRefresh))
}

// Directory traffic is at most one envelope per device per minute. It never
// enters the outbox: a later check sends the current list after any failure.
func pushDirectories(ctx context.Context, root string, client *gateway.Client, now time.Time) error {
	cfg, err := Load(root)
	if err != nil || cfg.GatewayURL() != client.Base {
		return err
	}
	path := filepath.Join(root, "remote", "dirsent.json")
	sent := map[string]dirSent{}
	if err := readJSON(path, &sent); err != nil {
		return err
	}
	next := map[string]dirSent{}
	changed := false
	for _, d := range cfg.Devices {
		if d.XKey == "" || !d.UnpairedAt.IsZero() {
			continue
		}
		next[d.ID] = sent[d.ID]
		bots, err := directoryFor(root, d)
		if err != nil {
			return err
		}
		state, due := directoryDue(bots, sent[d.ID], now)
		if !due {
			continue
		}
		body, _ := json.Marshal(directory{bots, now.Unix()})
		env, err := newBoxEnvelope(client.Key, d, "dir", body, now)
		if err != nil {
			continue
		}
		sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err = client.Send(sendCtx, env)
		cancel()
		if err == nil {
			next[d.ID], changed = state, true
		}
	}
	if !changed {
		return nil
	}
	raw, _ := json.Marshal(next)
	return fsutil.WriteAtomic(path, raw, 0600)
}

func runDirectories(ctx context.Context, root string, ready <-chan *gateway.Client, logf func(string, ...any)) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	var client *gateway.Client
	for {
		select {
		case <-ctx.Done():
			return
		case client = <-ready: // first check immediately after registration
			ticker.Reset(time.Minute)
		case <-ticker.C:
		case <-dirWake(root):
		}
		if client != nil {
			if err := pushDirectories(ctx, root, client, time.Now()); err != nil && logf != nil {
				logf("directory local state failure")
			}
		}
	}
}

// Receive holds the seen lock, which also serializes cache deletion on unpair.
func receiveDirectory(root, id string, body []byte, now time.Time) error {
	var dir directory
	if json.Unmarshal(body, &dir) != nil || len(dir.Bots) > 64 || dir.At <= 0 || dir.At > now.Add(5*time.Minute).Unix() {
		return nil
	}
	for i, b := range dir.Bots {
		if !bot.ValidName(b.Name) {
			return nil
		}
		// A bad seed costs that bot its portrait, not the whole directory.
		if !bot.ValidAvatarSeed(b.Seed) {
			dir.Bots[i].Seed = ""
		}
	}
	path := filepath.Join(root, "remote", "dirs", id+".json")
	var old dirCache
	if err := readJSON(path, &old); err != nil {
		return err
	}
	if dir.At <= old.At {
		return nil
	}
	// A delayed directory counts as seen when it was made, not when it arrived.
	raw, _ := json.Marshal(dirCache{dir, time.Unix(min(dir.At, now.Unix()), 0)})
	return fsutil.WriteAtomic(path, raw, 0600)
}

// RemoteBot is a directory entry visible to an owner of the calling bot.
type RemoteBot struct {
	Name string
	Seen time.Time
}

func (b RemoteBot) String(now time.Time) string {
	minutes := max(0, int(now.Sub(b.Seen)/time.Minute))
	return fmt.Sprintf("%s (remote, seen %dm ago)", b.Name, minutes)
}

func VisibleBots(root string, owners []string, now time.Time) ([]RemoteBot, error) {
	cfg, err := Load(root)
	if err != nil {
		return nil, err
	}
	var out []RemoteBot
	for _, d := range cfg.Devices {
		if d.Person == "" || !slices.Contains(owners, d.Person) || !d.UnpairedAt.IsZero() {
			continue
		}
		var cache dirCache
		if err := readJSON(filepath.Join(root, "remote", "dirs", d.ID+".json"), &cache); err != nil {
			return nil, err
		}
		if cache.Received.IsZero() || now.Sub(cache.Received) > 7*24*time.Hour {
			continue
		}
		for _, b := range cache.Bots[:min(64, len(cache.Bots))] {
			if bot.ValidName(b.Name) {
				out = append(out, RemoteBot{b.Name + "@" + d.Name, cache.Received})
			}
		}
	}
	return out, nil
}
