package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"xbot/internal/fsutil"
)

var ownerRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// Subscription is one browser PushManager subscription for an owner.
type Subscription struct {
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh"`
	Auth     string `json:"auth"`
}

type subFile struct {
	Subscriptions []Subscription `json:"subscriptions"`
}

func subsPath(root, owner string) (string, error) {
	if !ownerRe.MatchString(owner) {
		return "", errors.New("notify: bad owner")
	}
	return filepath.Join(root, "push", "subs", owner+".json"), nil
}

// Subscriptions reads the owner's push subscriptions. A missing file is empty.
func Subscriptions(root, owner string) ([]Subscription, error) {
	path, err := subsPath(root, owner)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f subFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	return f.Subscriptions, nil
}

// SaveSubscription upserts one subscription for the owner.
func SaveSubscription(root, owner string, sub Subscription) error {
	if err := checkSub(sub); err != nil {
		return err
	}
	path, err := subsPath(root, owner)
	if err != nil {
		return err
	}
	return fsutil.WithLock(path+".lock", func() error {
		cur, err := Subscriptions(root, owner)
		if err != nil {
			return err
		}
		replaced := false
		for i := range cur {
			if cur[i].Endpoint == sub.Endpoint {
				cur[i] = sub
				replaced = true
				break
			}
		}
		if !replaced {
			cur = append(cur, sub)
		}
		return writeSubs(path, cur)
	})
}

// RemoveSubscription drops one endpoint, or every subscription when endpoint is empty.
func RemoveSubscription(root, owner, endpoint string) error {
	path, err := subsPath(root, owner)
	if err != nil {
		return err
	}
	return fsutil.WithLock(path+".lock", func() error {
		cur, err := Subscriptions(root, owner)
		if err != nil || len(cur) == 0 {
			return err
		}
		if endpoint == "" {
			return os.Remove(path)
		}
		kept := cur[:0]
		for _, s := range cur {
			if s.Endpoint != endpoint {
				kept = append(kept, s)
			}
		}
		if len(kept) == 0 {
			return os.Remove(path)
		}
		return writeSubs(path, kept)
	})
}

func writeSubs(path string, subs []Subscription) error {
	raw, err := fsutil.Marshal(subFile{Subscriptions: subs})
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(path, raw, 0600)
}

func checkSub(sub Subscription) error {
	if err := validEndpoint(sub.Endpoint); err != nil {
		return err
	}
	pub, err := b64dec(sub.P256dh)
	if err != nil || len(pub) != 65 || pub[0] != 0x04 {
		return errors.New("notify: bad p256dh key")
	}
	auth, err := b64dec(sub.Auth)
	if err != nil || len(auth) != 16 {
		return errors.New("notify: bad auth secret")
	}
	return nil
}

func validEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return errors.New("notify: bad endpoint")
	}
	host := strings.Trim(strings.ToLower(u.Hostname()), "[]")
	if host == "" {
		return errors.New("notify: bad endpoint")
	}
	switch u.Scheme {
	case "http":
		if host == "127.0.0.1" || host == "localhost" || host == "::1" {
			return nil
		}
	case "https":
		if blockedHost(host) {
			return errors.New("notify: endpoint host is not a public push service")
		}
		return nil
	}
	return errors.New("notify: endpoint must be https")
}

func blockedHost(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || host == "metadata.google.internal" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return blockedIP(ip)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return false
	}
	for _, ip := range ips {
		if blockedIP(ip.IP) {
			return true
		}
	}
	return false
}

func blockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	// Carrier-grade NAT, 100.64.0.0/10.
	return ip4[0] == 100 && ip4[1]&0xc0 == 64
}
