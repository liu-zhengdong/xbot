package tools

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
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
// whitespace around the slash, folds fullwidth and small brackets, fullwidth
// underscores, and the extra slash characters, skips zero-width characters,
// ZWJ, ZWNJ, and variation selectors, and treats "&lt;" / numeric character
// references (including long runs of leading zeros) as "<".
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
	if !strings.ContainsAny(s, "<&") && !strings.Contains(s, "＜") && !strings.Contains(s, "﹤") {
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
	if foldTagRune(r) == '<' {
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

func isIgnorable(r rune) bool {
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
	switch r {
	case '<', '＜', '﹤':
		return '<'
	case '>', '＞', '﹥':
		return '>'
	case '/', '／', '∕', '⁄':
		return '/'
	case '_', '＿':
		return '_'
	}
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	if r >= 'a' && r <= 'z' {
		return r
	}
	if r >= 0xFF21 && r <= 0xFF3A {
		return r - 0xFF21 + 'a'
	}
	if r >= 0xFF41 && r <= 0xFF5A {
		return r - 0xFF41 + 'a'
	}
	return r
}
