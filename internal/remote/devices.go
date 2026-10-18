package remote

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/fsutil"
)

// SelfInfo is how this computer appears on its paired devices.
type SelfInfo struct {
	Name    string   `json:"name"`
	OS      string   `json:"os"`
	Bots    []string `json:"bots"`
	Gateway string   `json:"gateway"`
}

// DeviceView is one paired device for the 设备 drawer and the sidebar. Bots and
// Seen come from the device's last directory (G6); Seen is zero before one.
type DeviceView struct {
	ID    string    `json:"id"`
	Label string    `json:"label"`
	Name  string    `json:"name"` // ASCII address, used as bot@name
	Bots  []string  `json:"bots"`
	Seen  time.Time `json:"seen,omitzero"`
	Added time.Time `json:"added,omitzero"`
	// Unpaired reports that the other computer unpaired this one; the user
	// removes it from the 设备 drawer.
	Unpaired bool `json:"unpaired,omitempty"`
	// BotMessaging is the 设备 switch 「Bot 互发消息」, on by default.
	BotMessaging bool `json:"bot_messaging"`
}

// OnlineWindow: directories are re-sent every 15 minutes and within a minute of
// a change, so a device is online if one arrived within this window.
const OnlineWindow = 16 * time.Minute

func Self(root, personID string) (SelfInfo, error) {
	info, err := localPairInfo(root, personID)
	if err != nil {
		return SelfInfo{}, err
	}
	cfg, err := Load(root)
	return SelfInfo{Name: info.Name, OS: info.OS, Bots: info.Bots, Gateway: cfg.GatewayURL()}, err
}

func Devices(root string) ([]DeviceView, error) {
	cfg, err := Load(root)
	if err != nil {
		return nil, err
	}
	out := make([]DeviceView, 0, len(cfg.Devices))
	for _, d := range cfg.Devices {
		var cache dirCache
		if err := readJSON(filepath.Join(root, "remote", "dirs", d.ID+".json"), &cache); err != nil {
			return nil, err
		}
		v := DeviceView{ID: d.ID, Label: deviceLabel(d), Name: d.Name, Bots: []string{}, Seen: cache.Received, Added: d.Added, Unpaired: !d.UnpairedAt.IsZero(), BotMessaging: !d.NoBotMessaging}
		for _, b := range cache.Bots[:min(64, len(cache.Bots))] {
			if bot.ValidName(b.Name) {
				v.Bots = append(v.Bots, b.Name)
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// RenameDevice changes only the local label; the bot@name address stays.
func RenameDevice(root, id, label string) error {
	label = cleanLabel(label)
	if label == "" {
		return errors.New("remote: empty device name")
	}
	return update(root, func(c *Config) error {
		for i, d := range c.Devices {
			if d.ID == id {
				c.Devices[i].Label = label
				return nil
			}
		}
		return errors.New("remote: device not found")
	})
}

// SetBotMessaging flips 「Bot 互发消息」 for one paired device; off refuses box
// and bounce both ways, while the owner's own remote chat and directories stay.
func SetBotMessaging(root, id string, on bool) error {
	return update(root, func(c *Config) error {
		for i, d := range c.Devices {
			if d.ID == id {
				c.Devices[i].NoBotMessaging = !on
				return nil
			}
		}
		return errors.New("remote: device not found")
	})
}

// SetSelfName sets how this computer appears to devices paired from now on.
func SetSelfName(root, name string) error {
	name = cleanLabel(name)
	if name == "" {
		return errors.New("remote: empty device name")
	}
	raw, _ := json.Marshal(map[string]string{"name": name})
	return fsutil.WriteAtomic(filepath.Join(root, "remote", "device.json"), raw, 0600)
}

// NormalizeGateway adds https:// to a bare host and drops a trailing slash.
func NormalizeGateway(raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw != "" && !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	return raw
}

// CheckGateway is one reachability probe; it sends nothing identifying.
func CheckGateway(ctx context.Context, base string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/healthz", nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return errors.New("remote: gateway health check failed")
	}
	return nil
}
