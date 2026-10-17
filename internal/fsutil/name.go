package fsutil

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// nameRe accepts letters and digits from any script, plus '_' and '-' after the
// first rune. Path separators, dots, whitespace, and control runes never match,
// so a name is always one safe path element.
var nameRe = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}\p{M}_-]{0,63}$`)

// maxNameBytes keeps the longest 64-rune name inside one 255-byte filename.
const maxNameBytes = 255

// ValidName checks the shared Unicode name syntax and invisible-rune limits.
func ValidName(name string) bool {
	if !nameRe.MatchString(name) || len(name) > maxNameBytes {
		return false
	}
	for _, r := range name {
		if invisibleNameRune(r) {
			return false
		}
	}
	return true
}

// invisibleNameRune reports default-ignorable code points, including the
// variation selectors that unicode.Other_Default_Ignorable_Code_Point misses.
func invisibleNameRune(r rune) bool {
	if unicode.Is(unicode.Other_Default_Ignorable_Code_Point, r) {
		return true
	}
	switch {
	case r >= 0x180B && r <= 0x180D, // Mongolian free variation selectors
		r == 0x180F,
		r >= 0xFE00 && r <= 0xFE0F,   // variation selectors
		r >= 0xE0100 && r <= 0xE01EF: // variation selectors supplement
		return true
	}
	return false
}

// PortableName checks NFC and Windows device names for a syntactically valid name.
func PortableName(name string) bool {
	if !norm.NFC.IsNormalString(name) {
		return false
	}
	upper := strings.ToUpper(name)
	switch upper {
	case "CON", "PRN", "AUX", "NUL":
		return false
	}
	if strings.HasPrefix(upper, "COM") || strings.HasPrefix(upper, "LPT") {
		suffix := upper[3:]
		if len(suffix) == 1 && suffix[0] >= '0' && suffix[0] <= '9' || suffix == "¹" || suffix == "²" || suffix == "³" {
			return false
		}
	}
	return true
}
