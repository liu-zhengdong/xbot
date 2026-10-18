package remote

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
	"xbot/internal/bot"
	"xbot/internal/person"
	"xbot/internal/tools"
)

// gatewayToken is the self-host join token, sent only to an explicitly set
// gateway, never to the official default.
func gatewayToken(root string) string {
	cfg, err := Load(root)
	if err != nil {
		return ""
	}
	return cfg.GatewayToken(os.Getenv("XBOT_GATEWAY_TOKEN"))
}
func pairingPerson(root, id string) (string, error) {
	if id == "" {
		people, err := person.List(root)
		if err != nil {
			return "", err
		}
		if len(people) != 1 {
			return "", errors.New("remote: specify --person ID")
		}
		id = people[0].ID
	}
	_, err := person.Load(root, id)
	return id, err
}
func pairingGateway(root, raw string) error {
	if raw == "" {
		return nil
	}
	cfg, err := Load(root)
	if err != nil {
		return err
	}
	raw = strings.TrimRight(raw, "/")
	if len(cfg.Devices) != 0 && cfg.GatewayURL() != raw && !(isOfficial(raw) && cfg.Gateway == "") {
		return errors.New("remote: gateway mismatch")
	}
	return SetGateway(root, raw)
}
func cleanLabel(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
	s = tools.NeutralizeUntrusted(s)
	return strings.TrimSpace(string([]rune(s)[:min(40, len([]rune(s)))]))
}
func deviceLabel(d Device) string {
	if d.Label != "" {
		return d.Label
	}
	return d.Name
}
func labelAddress(label string) string {
	folded := norm.NFKD.String(label)
	folded = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, folded)
	name := strings.Trim(nonName.ReplaceAllString(folded, "-"), "-_")
	name = name[:min(64, len(name))]
	if !namePattern.MatchString(name) {
		return "device"
	}
	return name
}
func localPairInfo(root, personID string) (pairInfo, error) {
	personID, err := pairingPerson(root, personID)
	if err != nil {
		return pairInfo{}, err
	}
	var self struct {
		Name string `json:"name"`
	}
	if err := readJSON(filepath.Join(root, "remote", "device.json"), &self); err != nil {
		return pairInfo{}, err
	}
	if self.Name == "" {
		self.Name, _ = os.Hostname()
	}
	label := cleanLabel(self.Name)
	if label == "" {
		label = "device"
	}
	osName := runtime.GOOS
	switch osName {
	case "darwin":
		osName = "macOS"
	case "windows":
		osName = "Windows"
	case "linux":
		osName = "Linux"
	}
	list, err := directoryFor(root, Device{Person: personID})
	if err != nil {
		return pairInfo{}, err
	}
	info := pairInfo{Name: label, OS: osName, Bots: make([]string, 0, len(list))}
	for _, b := range list {
		info.Bots = append(info.Bots, b.Name)
	}
	return info, nil
}
func validPairInfo(info *pairInfo) bool {
	info.Name = cleanLabel(info.Name)
	if info.Name == "" || len(info.Bots) > 64 || len(info.OS) > 32 {
		return false
	}
	for _, b := range info.Bots {
		if !bot.ValidName(b) {
			return false
		}
	}
	info.OS = cleanLabel(info.OS)
	return true
}

// savePairDevice changes the table in one transaction. Reinstall preserves the
// address, display label, owner and position; retries for the same id are safe.
// The caller holds seen.json.lock, serializing directory deletion with Receive.
func savePairDevice(root, label, key, xkey, personID string, replace bool) (Device, error) {
	label = cleanLabel(label)
	d, err := newDevice(root, labelAddress(label), key, xkey, personID)
	if err != nil {
		return d, err
	}
	err = update(root, func(c *Config) error {
		for i, old := range c.Devices {
			if old.ID == d.ID {
				old.Key, old.XKey = key, xkey
				c.Devices[i], d = old, old
				return nil
			}
		}
		if replace {
			for i, old := range c.Devices {
				if deviceLabel(old) == label && old.Key != key && old.Person == d.Person {
					if err := os.Remove(filepath.Join(root, "remote", "dirs", old.ID+".json")); err != nil && !errors.Is(err, os.ErrNotExist) {
						return err
					}
					old.ID, old.Key, old.XKey = d.ID, key, xkey
					c.Devices[i], d = old, old
					return nil
				}
			}
		}
		base := label
		labelNumber := 0
		for n := 1; n <= 9; n++ {
			label = base
			if n > 1 {
				suffix := fmt.Sprintf(" %d", n)
				runes := []rune(base)
				label = string(runes[:min(len(runes), 40-len(suffix))]) + suffix
			}
			taken := false
			for _, old := range c.Devices {
				if deviceLabel(old) == label {
					taken = true
					break
				}
			}
			if !taken {
				labelNumber = n
				break
			}
		}
		if labelNumber == 0 {
			return errors.New("remote: device label suffixes exhausted")
		}
		d.Label = label
		base = d.Name
		for n := labelNumber; n <= 9; n++ {
			d.Name = base
			if n > 1 {
				d.Name = fmt.Sprintf("%.62s-%d", base, n)
			}
			if _, exists := c.ByName(d.Name); !exists {
				c.Devices = append(c.Devices, d)
				return nil
			}
		}
		return errors.New("remote: device name suffixes exhausted")
	})
	return d, err
}
