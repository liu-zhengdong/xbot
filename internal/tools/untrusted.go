package tools

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// wrapUntrusted marks external text as data, in the same form as link.Wrap.
// A page that contains the closer cannot end the block early.
func wrapUntrusted(source, typ, sender string, at time.Time, body string) string {
	body = NeutralizeUntrusted(body)
	sender = strings.Map(func(r rune) rune {
		if r < 32 || r == '"' || r == '<' || r == '>' {
			return -1
		}
		return r
	}, sender)
	if at.IsZero() {
		at = time.Now()
	}
	return fmt.Sprintf("<untrusted_event source=%q type=%q sender=%q time=%q>\n%s\n</untrusted_event>",
		source, typ, sender, at.UTC().Format(time.RFC3339), body)
}

// NeutralizeUntrusted rewrites untrusted_event openers and closers so a
// payload cannot end or forge the fence. The replacement is not a tag:
// "[/untrusted_event]" does not match a later closer scan, and it does not
// leave a ">" that completes one. Matching is ASCII-case-insensitive, allows
// whitespace around the slash, folds every bracket lookalike IsLT and IsGT
// know, compatibility forms (fullwidth, mathematical and circled letters),
// Cyrillic and Greek letters that look like Latin ones, and the extra slash
// characters, skips zero-width characters, ZWJ, ZWNJ, variation selectors and
// combining marks, and treats "&lt;" / numeric character references
// (including long runs of leading zeros) as "<".
func NeutralizeUntrusted(s string) string {
	s = rewriteTag(s, "untrusted_event", true, "[/untrusted_event]")
	return rewriteTag(s, "untrusted_event", false, "[untrusted_event]")
}

// NeutralizeCloser rewrites a closing tag to "</ name" without consuming a
// following ">". "</NAME>", "</ name >", and "< / name" all match.
// project_instructions uses this form; untrusted_event does not.
func NeutralizeCloser(s, name string) string {
	return rewriteTag(s, name, true, "</ "+strings.ToLower(name))
}

// NeutralizeOpener rewrites an opening tag to "< name". A slash after "<"
// is a closer and is left for NeutralizeCloser.
func NeutralizeOpener(s, name string) string {
	return rewriteTag(s, name, false, "< "+strings.ToLower(name))
}

func rewriteTag(s, name string, closer bool, repl string) string {
	name = strings.ToLower(name)
	if name == "" || s == "" {
		return s
	}
	if !mayOpenTag(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	changed := false
	i := 0
	for i < len(s) {
		end, ok := matchTag(s[i:], name, closer)
		if !ok {
			_, size := utf8.DecodeRuneInString(s[i:])
			b.WriteString(s[i : i+size])
			i += size
			continue
		}
		b.WriteString(repl)
		i += end
		changed = true
	}
	if !changed {
		return s
	}
	return b.String()
}

// matchTag matches "<" (or an entity for "<") + optional space + optional "/"
// + optional space + name at the start of s. closer requires the slash.
// Zero-width characters are skipped. The returned length does not include a
// trailing ">".
func matchTag(s, name string, closer bool) (int, bool) {
	i, ok := matchLT(s)
	if !ok {
		return 0, false
	}
	i = skipSpace(s, i)
	if i >= len(s) {
		return 0, false
	}
	r, size := utf8.DecodeRuneInString(s[i:])
	if foldTagRune(r) == '/' {
		if !closer {
			return 0, false
		}
		i += size
		i = skipSpace(s, i)
	} else if closer {
		return 0, false
	}
	for _, want := range name {
		i = skipIgnorable(s, i)
		if i >= len(s) {
			return 0, false
		}
		r, size = utf8.DecodeRuneInString(s[i:])
		if foldTagRune(r) != want {
			return 0, false
		}
		i += size
	}
	return i, true
}

func matchLT(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	r, size := utf8.DecodeRuneInString(s)
	if IsLT(r) {
		return size, true
	}
	if r != '&' {
		return 0, false
	}
	if n, ok := matchNamed(s, "lt"); ok {
		return n, true
	}
	if n, ok := matchNumeric(s, '<'); ok {
		return n, true
	}
	return 0, false
}

func matchNamed(s, name string) (int, bool) {
	need := 1 + len(name) + 1
	if len(s) < need || s[0] != '&' {
		return 0, false
	}
	if !strings.EqualFold(s[1:1+len(name)], name) || s[1+len(name)] != ';' {
		return 0, false
	}
	return need, true
}

func matchNumeric(s string, want rune) (int, bool) {
	if len(s) < 4 || s[0] != '&' || s[1] != '#' {
		return 0, false
	}
	i := 2
	hex := false
	if s[i] == 'x' || s[i] == 'X' {
		hex = true
		i++
	}
	// Leading zeros do not count toward the digit budget. &#00000060; is still
	// U+003C. Significant digits stop at a rune; another digit means the
	// reference is larger than a Unicode scalar and is not a match.
	for i < len(s) && s[i] == '0' {
		i++
	}
	val := 0
	digits := 0
	base := 10
	if hex {
		base = 16
	}
	for i < len(s) {
		d, ok := hexDigit(s[i], hex)
		if !ok {
			break
		}
		digits++
		if val > (unicode.MaxRune-d)/base {
			return 0, false
		}
		val = val*base + d
		i++
	}
	if digits == 0 || i >= len(s) || s[i] != ';' || rune(val) != want {
		return 0, false
	}
	return i + 1, true
}

func hexDigit(c byte, hex bool) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case hex && c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	case hex && c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	default:
		return 0, false
	}
}

func skipSpace(s string, i int) int {
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !unicode.IsSpace(r) && !isIgnorable(r) {
			break
		}
		i += size
	}
	return i
}

func skipIgnorable(s string, i int) int {
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !isIgnorable(r) {
			break
		}
		i += size
	}
	return i
}

// mayOpenTag reports whether s has anything matchLT could start on.
func mayOpenTag(s string) bool {
	for _, r := range s {
		if r == '&' || IsLT(r) {
			return true
		}
	}
	return false
}

// IsLT reports whether r reads as "<": the ASCII bracket and the lookalikes a
// model may take for one (fullwidth, small, angle, vertical, single guillemet,
// ornamental, modifier and syllabics forms). No other rune folds to "<", so a
// tag can only start on one of these.
func IsLT(r rune) bool {
	switch r {
	case '<', '\uff1c', '\ufe64', '\u2329', '\u3008', '\ufe3f', '\u27e8', '\u2039', '\u00ab', '\u276c', '\u276e', '\u2770', '\u02c2', '\u1438', '\u29fc':
		return true
	}
	return false
}

// IsGT is IsLT for ">".
func IsGT(r rune) bool {
	switch r {
	case '>', '\uff1e', '\ufe65', '\u232a', '\u3009', '\ufe40', '\u27e9', '\u203a', '\u00bb', '\u276d', '\u276f', '\u2771', '\u02c3', '\u1433', '\u29fd':
		return true
	}
	return false
}

func isIgnorable(r rune) bool {
	if r >= 0x300 && unicode.Is(unicode.Mn, r) {
		return true
	}
	switch r {
	case '\u200b', '\u200c', '\u200d', '\u2060', '\u2061', '\u2062', '\u2063', '\u2064', '\ufeff', '\u00ad':
		return true
	}
	if r >= '\ufe00' && r <= '\ufe0f' {
		return true
	}
	if r >= 0xE0100 && r <= 0xE01EF {
		return true
	}
	return false
}

func foldTagRune(r rune) rune {
	if r < utf8.RuneSelf {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}
	switch {
	case IsLT(r):
		return '<'
	case IsGT(r):
		return '>'
	}
	switch r {
	case '∕', '⁄', '⧸', '╱':
		return '/'
	}
	if c, ok := latinLookalike[r]; ok {
		return c
	}
	// Compatibility forms: fullwidth, mathematical and circled letters, small
	// forms. Only a single-rune decomposition counts.
	var buf [utf8.UTFMax]byte
	n := utf8.EncodeRune(buf[:], r)
	if d := norm.NFKC.Properties(buf[:n]).Decomposition(); len(d) > 0 {
		if c, size := utf8.DecodeRune(d); size == len(d) && c != r {
			return foldTagRune(c)
		}
	}
	return r
}

// latinLookalike maps Cyrillic, Greek and Armenian letters that look like the
// Latin letters of a fence name to those letters.
var latinLookalike = map[rune]rune{
	'а': 'a', 'А': 'a', 'Α': 'a',
	'с': 'c', 'С': 'c',
	'ԁ': 'd',
	'е': 'e', 'Е': 'e', 'Ε': 'e',
	'һ': 'h',
	'і': 'i', 'І': 'i', 'Ι': 'i', 'ι': 'i', 'ı': 'i',
	'ј': 'j', 'Ј': 'j',
	'Ν': 'n', 'ո': 'n',
	'о': 'o', 'О': 'o', 'ο': 'o', 'Ο': 'o',
	'р': 'p', 'Р': 'p', 'ρ': 'p', 'Ρ': 'p',
	'г': 'r',
	'ѕ': 's', 'Ѕ': 's',
	'Т': 't', 'Τ': 't',
	'ս': 'u',
	'ν': 'v',
	'х': 'x', 'Х': 'x', 'Χ': 'x',
	'у': 'y',
}
