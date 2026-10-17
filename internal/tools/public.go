package tools

import (
	"encoding/binary"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// PublicIP reports whether ip is a global unicast address a public fetch may
// dial. Loopback, private, link-local (including cloud metadata), and the
// other IANA special-purpose ranges are refused.
func PublicIP(ip net.IP) bool {
	if len(ip) == 0 {
		return false
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() ||
		addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	list := special6
	if addr.Is4() {
		list = special4
	}
	for _, p := range list {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}

// IANA ranges IsGlobalUnicast and IsPrivate do not cover. Same set the push
// client refuses, so a fetch cannot reach metadata or documentation space.
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

// literalIP parses a host that is already an address: dotted or colon form,
// a decimal 32-bit integer, or a 0x hex integer. ok is false when host is a name.
func literalIP(host string) (net.IP, bool) {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" {
		return nil, false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip, true
	}
	if n, err := strconv.ParseUint(host, 10, 32); err == nil && allDigits(host) {
		return uint32IP(uint32(n)), true
	}
	if (strings.HasPrefix(host, "0x") || strings.HasPrefix(host, "0X")) && len(host) > 2 {
		if n, err := strconv.ParseUint(host, 0, 32); err == nil {
			return uint32IP(uint32(n)), true
		}
	}
	return nil, false
}

func uint32IP(n uint32) net.IP {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], n)
	return net.IPv4(b[0], b[1], b[2], b[3]).To4()
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// blockedName refuses hostnames that are not public destinations even before
// DNS. A resolver that maps them to a public address is still not a page we fetch.
func blockedName(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	switch h {
	case "localhost", "metadata", "metadata.google.internal", "metadata.google.com":
		return true
	}
	return strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".internal")
}
