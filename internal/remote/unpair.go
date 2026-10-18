package remote

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"xbot/internal/gateway"
)

// Unpairing tells the other computer in one best-effort encrypted envelope of
// kind unpair: no outbox, no retry, and any failure (no gateway, timeout, an
// error) never blocks or fails the local removal.

// unpairTimeout keeps a slow gateway from holding up the unpair.
const unpairTimeout = 5 * time.Second

func RemoveDeviceID(root, id string) error {
	return unpairDevice(root, func(d Device) bool { return d.ID == id }, id)
}

// notifyUnpair sends the kind unpair envelope to d. It reports nothing, so a
// failure can neither fail the local removal nor reach a log.
func notifyUnpair(ctx context.Context, root string, d Device, now time.Time) {
	if d.XKey == "" || ctx == nil {
		return
	}
	cfg, err := Load(root)
	if err != nil {
		return
	}
	priv, _, _, err := Identity(root)
	if err != nil {
		return
	}
	body, err := json.Marshal(struct {
		At int64 `json:"at"`
	}{now.Unix()})
	if err != nil {
		return
	}
	env, err := newBoxEnvelope(priv, d, "unpair", body, now)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, unpairTimeout)
	defer cancel()
	_ = (&gateway.Client{Base: cfg.GatewayURL(), Key: priv}).Send(ctx, env)
}

// receiveUnpair handles kind unpair from a paired device; the body is
// {"at": <time>}. The device stays, marked as unpaired by the other computer,
// and its directory goes so the sidebar drops it.
func receiveUnpair(root, id string, body []byte, now time.Time) error {
	var u struct {
		At int64 `json:"at"`
	}
	if json.Unmarshal(body, &u) != nil {
		return nil
	}
	return update(root, func(c *Config) error {
		for i := range c.Devices {
			if c.Devices[i].ID == id {
				c.Devices[i].UnpairedAt = now.UTC()
				if err := os.Remove(filepath.Join(root, "remote", "dirs", id+".json")); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
		}
		return nil
	})
}
