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
// payload cannot end or forge the fence. Matching is ASCII-case-insensitive,
// allows whitespace around the slash, and treats fullwidth ＜ ／ ＞ and
// fullwidth Latin letters as their ASCII forms.
func NeutralizeUntrusted(s string) string {
	s = NeutralizeCloser(s, "untrusted_event")
	return NeutralizeOpener(s, "untrusted_event")
}

// NeutralizeCloser rewrites a closing tag to "</ name" without consuming a
// following ">". "</NAME>", "</ name >", and "< / name" all match.
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
	if !strings.Contains(s, "<") && !strings.Contains(s, "＜") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	changed := false
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if foldTagRune(r) != '<' {
			b.WriteString(s[i : i+size])
			i += size
			continue
		}
		end, ok := matchTag(s[i:], name, closer)
		if !ok {
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

// matchTag matches "<" + optional space + optional "/" + optional space + name
// at the start of s. closer requires the slash. The returned length does not
// include a trailing ">".
func matchTag(s, name string, closer bool) (int, bool) {
	i := 0
	r, size := utf8.DecodeRuneInString(s[i:])
	if foldTagRune(r) != '<' {
		return 0, false
	}
	i += size
	i = skipSpace(s, i)
	if i >= len(s) {
		return 0, false
	}
	r, size = utf8.DecodeRuneInString(s[i:])
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

func skipSpace(s string, i int) int {
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !unicode.IsSpace(r) {
			break
		}
		i += size
	}
	return i
}

func foldTagRune(r rune) rune {
	switch r {
	case '<', '＜':
		return '<'
	case '>', '＞':
		return '>'
	case '/', '／':
		return '/'
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
