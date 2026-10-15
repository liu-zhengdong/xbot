package bot

import "strings"

// SilentText reports a final reply that should not be shown to the owner.
func SilentText(s string) bool {
	return strings.TrimSpace(s) == "SILENT"
}

// SilentPrefix reports whether s is still a prefix of SILENT, including whitespace.
// Callers hold those bytes until the reply finishes or more text arrives.
func SilentPrefix(s string) bool {
	return strings.HasPrefix("SILENT", strings.TrimSpace(s))
}
