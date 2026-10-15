package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
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
	if host == "" || integerHost(host) {
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
	ips, err := lookupHost(ctx, host)
	if err != nil || len(ips) == 0 {
		return true
	}
	for _, ip := range ips {
		if blockedIP(ip) {
			return true
		}
	}
	return false
}

// lookupHost resolves a push hostname. Tests replace it.
var lookupHost = defaultLookupHost

func defaultLookupHost(ctx context.Context, host string) ([]net.IP, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	out := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		if a.IP != nil {
			out = append(out, a.IP)
		}
	}
	return out, nil
}

// SetLookupHost replaces hostname resolution. A nil fn restores the default.
// Tests use it so a fake DNS answer cannot decide the result.
func SetLookupHost(fn func(context.Context, string) ([]net.IP, error)) {
	if fn == nil {
		lookupHost = defaultLookupHost
		return
	}
	lookupHost = fn
}

// integerHost is a decimal or hex address written as one integer, such as 2130706433.
func integerHost(host string) bool {
	if host == "" {
		return false
	}
	if strings.HasPrefix(host, "0x") || strings.HasPrefix(host, "0X") {
		rest := host[2:]
		return rest != "" && isHex(rest)
	}
	return isDigits(host)
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// publicClient dials endpoint with a control hook that rejects a non-public address.
// A client that already has a Transport is left alone so tests can use httptest.
func publicClient(endpoint string, client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{Timeout: 4 * time.Second}
	}
	if client.Transport != nil {
		return client
	}
	timeout := client.Timeout
	if timeout <= 0 {
		timeout = 4 * time.Second
	}
	return &http.Client{Timeout: timeout, Transport: publicTransport(endpoint)}
}

func publicTransport(endpoint string) *http.Transport {
	allowLoop := false
	if u, err := url.Parse(endpoint); err == nil && u.Scheme == "http" {
		host := strings.Trim(strings.ToLower(u.Hostname()), "[]")
		if host == "127.0.0.1" || host == "localhost" || host == "::1" {
			allowLoop = true
		}
	}
	d := &net.Dialer{Timeout: 4 * time.Second, Control: dialControl(allowLoop)}
	return &http.Transport{DialContext: d.DialContext}
}

func dialControl(allowLoop bool) func(string, string, syscall.RawConn) error {
	return func(network, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return errors.New("notify: bad dial address")
		}
		ip := net.ParseIP(host)
		if ip == nil || blockedIP(ip) {
			if allowLoop && ip != nil && ip.IsLoopback() {
				return nil
			}
			return errors.New("notify: endpoint host is not a public push service")
		}
		return nil
	}
}

// IANA special-purpose ranges that are not public destinations.
// IsGlobalUnicast and IsPrivate do not cover all of them.
var (
	special4 = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("192.31.196.0/24"),
		netip.MustParsePrefix("192.52.193.0/24"),
		netip.MustParsePrefix("192.88.99.0/24"),
		netip.MustParsePrefix("192.175.48.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("240.0.0.0/4"),
	}
	special6 = []netip.Prefix{
		netip.MustParsePrefix("64:ff9b::/96"),
		netip.MustParsePrefix("64:ff9b:1::/48"),
		netip.MustParsePrefix("100::/64"),
		netip.MustParsePrefix("2001::/32"),
		netip.MustParsePrefix("2001:2::/48"),
		netip.MustParsePrefix("2001:db8::/32"),
		netip.MustParsePrefix("2001:10::/28"),
		netip.MustParsePrefix("2001:20::/28"),
		netip.MustParsePrefix("2002::/16"),
	}
)

func blockedIP(ip net.IP) bool {
	if len(ip) == 0 {
		return true
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return true
	}
	list := special6
	if addr.Is4() {
		list = special4
	}
	for _, p := range list {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
