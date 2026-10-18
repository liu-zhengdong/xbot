// Package remote connects paired installs through a signed mailbox relay.
package remote

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

const DefaultGateway = "https://xbot.babelgo.cn"

type Device struct {
	Name   string    `json:"name"`
	Label  string    `json:"label,omitempty"`
	ID     string    `json:"id"`
	Key    string    `json:"key"`
	XKey   string    `json:"xkey,omitempty"`
	Person string    `json:"person"`
	Added  time.Time `json:"added"`
	// UnpairedAt is when the other computer said it unpaired this device (kind
	// unpair). Local removal is still the user's call, so the device stays but
	// sends and receives nothing until it is removed or paired again.
	UnpairedAt time.Time `json:"unpaired_at,omitzero"`
}
type Config struct {
	Gateway string   `json:"gateway"`
	Devices []Device `json:"devices"`
}

// GatewayURL resolves the default without persisting it in the device table.
func (c Config) GatewayURL() string {
	if c.Gateway != "" {
		return c.Gateway
	}
	return DefaultGateway
}

// GatewayToken scopes a self-host join token to an explicitly configured gateway, so it never
// reaches the official one (which takes no token).
func (c Config) GatewayToken(token string) string {
	if c.Gateway == "" {
		return ""
	}
	return token
}

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// Identity loads or creates this install's key; LoadOrCreateKey holds the
// file lock, so it must not be taken here too (flock is per open file).
func Identity(root string) (priv ed25519.PrivateKey, id, keyB64 string, err error) {
	priv, err = gateway.LoadOrCreateKey(filepath.Join(root, "keys", "remote", "install.key"))
	if err == nil {
		pub := priv.Public().(ed25519.PublicKey)
		id, keyB64 = gateway.ID(pub), base64.StdEncoding.EncodeToString(pub)
	}
	return
}

// XIdentity deterministically derives X25519 keys from the Ed25519 seed.
func XIdentity(root string) (*ecdh.PrivateKey, string, error) {
	priv, _, _, err := Identity(root)
	if err != nil {
		return nil, "", err
	}
	key, err := ecdh.X25519().NewPrivateKey(hmacSHA256(priv.Seed(), "xbot-x25519-v1"))
	if err != nil {
		return nil, "", err
	}
	return key, base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()), nil
}

func readJSON(path string, v any) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return err
	}
	if len(raw) > 1<<20 {
		return errors.New("remote: state exceeds 1 MiB")
	}
	return json.Unmarshal(raw, v)
}

func Load(root string) (cfg Config, err error) {
	err = readJSON(filepath.Join(root, "remote", "devices.json"), &cfg)
	if isOfficial(cfg.Gateway) {
		cfg.Gateway = "" // older configs stored the official URL explicitly
	}
	return
}

// isOfficial reports whether raw names the official gateway in any spelling
// (case, default port, trailing slash), so it is treated as the default.
func isOfficial(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || raw == "" {
		return false
	}
	def, _ := url.Parse(DefaultGateway)
	return strings.EqualFold(u.Scheme, def.Scheme) && strings.EqualFold(u.Hostname(), def.Hostname()) &&
		(u.Port() == "" || u.Port() == "443") && strings.Trim(u.Path, "/") == "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

func (c Config) ByName(name string) (Device, bool) {
	for _, d := range c.Devices {
		if d.Name == name {
			return d, true
		}
	}
	return Device{}, false
}
func (c Config) ByID(id string) (Device, bool) {
	for _, d := range c.Devices {
		if d.ID == id {
			return d, true
		}
	}
	return Device{}, false
}

func update(root string, fn func(*Config) error) error {
	path := filepath.Join(root, "remote", "devices.json")
	return fsutil.WithLock(path+".lock", func() error {
		cfg, err := Load(root)
		if err != nil {
			return err
		}
		if err := fn(&cfg); err != nil {
			return err
		}
		raw, err := json.Marshal(cfg)
		if err != nil {
			return err
		}
		if len(raw) > 1<<20 {
			return errors.New("remote: state exceeds 1 MiB")
		}
		return fsutil.WriteAtomic(path, raw, 0600)
	})
}

// fillXKey records the X25519 key of a paired device that has none (added
// without --xkey); a different existing key is left alone.
func fillXKey(root, id, xkey string) error {
	return update(root, func(c *Config) error {
		for i := range c.Devices {
			if c.Devices[i].ID == id && c.Devices[i].XKey == "" {
				c.Devices[i].XKey = xkey
			}
		}
		return nil
	})
}

func newDevice(root, name, keyB64, xkeyB64, personID string) (d Device, err error) {
	if !namePattern.MatchString(name) {
		return d, errors.New("remote: invalid device name")
	}
	pub, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return d, errors.New("remote: invalid public key")
	}
	if xkeyB64 != "" && !isB64(xkeyB64, 32) {
		return d, errors.New("remote: invalid xkey")
	}
	_, self, _, err := Identity(root)
	if err != nil {
		return d, err
	}
	if gateway.ID(pub) == self {
		return d, errors.New("remote: cannot pair own install")
	}
	if personID == "" {
		people, err := person.List(root)
		if err != nil {
			return d, err
		}
		if len(people) != 1 {
			return d, errors.New("remote: specify --person ID")
		}
		personID = people[0].ID
	}
	if _, err := person.Load(root, personID); err != nil {
		return d, err
	}
	d = Device{Name: name, ID: gateway.ID(pub), Key: base64.StdEncoding.EncodeToString(pub), XKey: xkeyB64, Person: personID, Added: time.Now().UTC()}
	return d, nil
}

func AddDevice(root, name, keyB64, xkeyB64, personID string) (d Device, err error) {
	d, err = newDevice(root, name, keyB64, xkeyB64, personID)
	if err != nil {
		return d, err
	}
	err = update(root, func(c *Config) error {
		for _, existing := range c.Devices {
			if existing.Name == name || existing.ID == d.ID {
				return errors.New("remote: duplicate device name or id")
			}
		}
		c.Devices = append(c.Devices, d)
		return nil
	})
	if err != nil {
		return Device{}, err
	}
	clearUnpaired(root, d.ID) // A new pairing replaces any earlier "unpaired by the peer".
	return d, nil
}

// clearUnpaired drops the unpair mark left by an earlier pairing of the same device.
func clearUnpaired(root, id string) {
	_ = update(root, func(c *Config) error {
		for i := range c.Devices {
			if c.Devices[i].ID == id {
				c.Devices[i].UnpairedAt = time.Time{}
			}
		}
		return nil
	})
}

func RemoveDevice(root, name string) error {
	return unpairDevice(root, func(d Device) bool { return d.Name == name }, fmt.Sprintf("%q", name))
}

// unpairDevice tells the other computer first (best effort, never required)
// and then removes locally, so both sides learn even when the send is slow.
func unpairDevice(root string, match func(Device) bool, what string) error {
	cfg, err := Load(root)
	if err != nil {
		return err
	}
	for _, d := range cfg.Devices {
		if match(d) {
			notifyUnpair(context.Background(), root, d, time.Now())
			break
		}
	}
	return removeDevice(root, match, what)
}

// removeDevice matches inside the locked update, so a device replaced while
// the caller waited is never the one removed.
func removeDevice(root string, match func(Device) bool, what string) error {
	return fsutil.WithLock(filepath.Join(root, "remote", "seen.json.lock"), func() error {
		return update(root, func(c *Config) error {
			for i, d := range c.Devices {
				if match(d) {
					if err := os.Remove(filepath.Join(root, "remote", "dirs", d.ID+".json")); err != nil && !errors.Is(err, os.ErrNotExist) {
						return err
					}
					c.Devices = append(c.Devices[:i], c.Devices[i+1:]...)
					return nil
				}
			}
			return fmt.Errorf("remote: device %s not found", what)
		})
	})
}

func SetGateway(root, raw string) error {
	if raw == "default" {
		raw = ""
	}
	if raw != "" {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("remote: gateway must be an http(s) URL with a host")
		}
	}
	raw = strings.TrimRight(raw, "/")
	if isOfficial(raw) {
		raw = "" // Stored as the default so the idle privacy rule still applies.
	}
	err := update(root, func(c *Config) error { c.Gateway = raw; return nil })
	if err == nil {
		wakeRun(root) // A loop idling without a gateway starts now.
	}
	return err
}
