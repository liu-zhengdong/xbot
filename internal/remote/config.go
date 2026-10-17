// Package remote connects paired installs through a signed mailbox relay.
package remote

import (
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

type Device struct {
	Name   string    `json:"name"`
	ID     string    `json:"id"`
	Key    string    `json:"key"`
	Person string    `json:"person"`
	Added  time.Time `json:"added"`
}
type Config struct {
	Gateway string   `json:"gateway"`
	Devices []Device `json:"devices"`
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
	return
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

func AddDevice(root, name, keyB64, personID string) (d Device, err error) {
	if !namePattern.MatchString(name) {
		return d, errors.New("remote: invalid device name")
	}
	pub, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return d, errors.New("remote: invalid public key")
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
	d = Device{Name: name, ID: gateway.ID(pub), Key: base64.StdEncoding.EncodeToString(pub), Person: personID, Added: time.Now().UTC()}
	err = update(root, func(c *Config) error {
		for _, existing := range c.Devices {
			if existing.Name == name || existing.ID == d.ID {
				return errors.New("remote: duplicate device name or id")
			}
		}
		c.Devices = append(c.Devices, d)
		return nil
	})
	return
}

func RemoveDevice(root, name string) error {
	return update(root, func(c *Config) error {
		for i, d := range c.Devices {
			if d.Name == name {
				c.Devices = append(c.Devices[:i], c.Devices[i+1:]...)
				return nil
			}
		}
		return fmt.Errorf("remote: device %q not found", name)
	})
}

func SetGateway(root, raw string) error {
	if raw != "" {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("remote: gateway must be an http(s) URL with a host")
		}
	}
	return update(root, func(c *Config) error { c.Gateway = strings.TrimRight(raw, "/"); return nil })
}
