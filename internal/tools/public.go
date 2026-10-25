package tools

import (
	"encoding/binary"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// Dialable reports whether a web fetch may dial ip. Only the addresses that
// reach this machine itself or the cloud metadata service are refused:
// loopback, unspecified (0.0.0.0/8 and ::), link-local (169.254.0.0/16 and
// fe80::/10, which holds the metadata address) and multicast. Everything else
// is allowed on purpose. A local proxy that rewrites every name into
// 198.18.0.0/15 or another range must keep working, so private, shared and
// benchmark space is not an error.
func Dialable(ip net.IP) bool {
	if len(ip) == 0 {
		return false
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsValid() || addr.IsLoopback() || addr.IsUnspecified() ||
		addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() {
		return false
	}
	if addr.Is4() && addr.As4()[0] == 0 {
		return false
	}
	return true
}

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

// blockedName refuses hostnames that name this machine or the metadata service
// even before DNS. Other names, including .local and .internal, are left to the
// address check.
func blockedName(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	switch h {
	case "localhost", "metadata", "metadata.google.internal", "metadata.google.com":
		return true
	}
	return strings.HasSuffix(h, ".localhost")
}
