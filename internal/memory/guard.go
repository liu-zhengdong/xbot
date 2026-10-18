package memory

import (
	"strings"
	"unicode/utf8"
)

const untrustedMarker = " (untrusted, recorded by "
const ownerMarker = " (owner-recorded"

// Provenance marks a fact as data recorded by someone else.
// The suffix is applied again only when it is not already the end of the text.
// The word "untrusted" earlier in the fact does not count.
func Provenance(text, by, device string) string {
	by = label(by)
	if by == "" {
		by = "unknown"
	}
	device = label(device)
	suffix := untrustedMarker + by + ")"
	if device != "" && device != "local" {
		suffix = untrustedMarker + by + " on " + device + ")"
	}
	if strings.HasSuffix(text, suffix) {
		return text
	}
	// Remember's size check runs after this call, on the text plus suffix.
	// Clip the text so text+suffix fits in maxFactBytes.
	return clipTo(text, maxFactBytes-len(suffix)) + suffix
}

// OwnerProvenance marks a fact the owner wrote on another linked device.
// The suffix is applied again only when it is not already the end of the text.
func OwnerProvenance(text, device string) string {
	device = label(device)
	suffix := ownerMarker + ")"
	if device != "" && device != "local" && device != "relay" {
		suffix = ownerMarker + " on " + device + ")"
	}
	if strings.HasSuffix(text, suffix) {
		return text
	}
	return clipTo(text, maxFactBytes-len(suffix)) + suffix
}

func clipTo(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) && len(s) > 0 {
		s = s[:len(s)-1]
	}
	return s
}

func label(s string) string {
	s = oneLine(s)
	if s == "" || hasControl(s) {
		return ""
	}
	if len(s) > 80 {
		s = s[:80]
		for !utf8.ValidString(s) && len(s) > 0 {
			s = s[:len(s)-1]
		}
	}
	return s
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}
