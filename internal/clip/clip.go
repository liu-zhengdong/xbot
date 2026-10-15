// Package clip bounds untrusted or tool-produced text before it enters a transcript.
package clip

import (
	"strings"
	"unicode/utf8"
)

const marker = "\n[truncated]"

// Valid replaces invalid UTF-8 so the result can be stored as JSON.
func Valid(s string) string {
	return strings.ToValidUTF8(s, "\uFFFD")
}

// Bytes returns s limited to at most max bytes, on a rune boundary.
// A non-positive max disables the limit. The bool reports whether s was cut.
func Bytes(s string, max int) (string, bool) {
	s = Valid(s)
	if max <= 0 || len(s) <= max {
		return s, false
	}
	budget := max - len(marker)
	if budget < 0 {
		budget = 0
	}
	if budget > len(s) {
		budget = len(s)
	}
	for budget > 0 && !utf8.RuneStart(s[budget]) {
		budget--
	}
	return s[:budget] + marker, true
}
