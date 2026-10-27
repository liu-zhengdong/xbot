package memory

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// labels are lead-ins that file a fact under a heading without changing it, so
// "约定：X" repeats "X". Any other lead-in may be a qualifier and keeps the fact.
var labels = map[string]bool{"约定": true, "备注": true, "规则": true, "决定": true, "note": true, "rule": true, "decision": true}

// fillers carry no meaning in a fact; dropping them lets "简短的回答" match "简短回答"
// and "已经改为" match "改为". They run before synonyms, which do not chain.
var fillers = strings.NewReplacer("已经", "", "已", "", "的", "", "了", "", "地", "", "得", "")

// synonyms fold rewordings an extractor rotates through onto one spelling.
// Longer phrases come first so "设置为" is not split by "置为".
var synonyms = strings.NewReplacer(
	"设置为", "设为", "更新为", "设为", "修改为", "设为", "调整为", "设为",
	"改为", "设为", "改成", "设为", "定为", "设为", "换成", "设为",
	"更喜欢", "偏好", "喜欢", "偏好", "倾向于", "偏好", "倾向", "偏好", "习惯用", "偏好用",
	"has been set to", "set to", "was set to", "set to", "has been changed to", "set to",
	"was changed to", "set to", "changed to", "set to", "has been updated to", "set to",
	"was updated to", "set to", "updated to", "set to", "is now", "set to",
	"likes", "prefers",
)

// Bare is the fact text without the provenance suffix Provenance or OwnerProvenance added.
func Bare(text string) string {
	for _, m := range []string{untrustedMarker, ownerMarker} {
		if i := strings.LastIndex(text, m); i >= 0 && strings.HasSuffix(text, ")") {
			text = text[:i]
		}
	}
	return strings.TrimSpace(text)
}

// NearDuplicate reports whether two facts record the same thing: equal once the
// provenance suffix, case, punctuation, filler words, common rewordings and a
// heading label such as "约定：" are removed. Anything else that differs, even one
// name, count or negation in a long sentence, may be a correction and keeps both;
// numbers, quoted values and paths are compared before fillers are dropped.
func NearDuplicate(a, b string) bool {
	a, b = Bare(a), Bare(b)
	if !sameSet(anchors(a), anchors(b)) {
		return false
	}
	na, nb := normFact(a), normFact(b)
	if na == "" || nb == "" {
		return false
	}
	return na == nb || normFact(unlabel(a)) == nb || na == normFact(unlabel(b))
}

// unlabel drops a leading heading label and its colon.
func unlabel(s string) string {
	i := strings.IndexAny(s, "：:")
	if i < 0 || !labels[strings.ToLower(strings.TrimSpace(s[:i]))] {
		return s
	}
	_, n := utf8.DecodeRuneInString(s[i:])
	return s[i+n:]
}

// normFact lowercases, folds rewordings and keeps letters, digits, a few path
// characters and single spaces between ASCII words, without English articles.
func normFact(s string) string {
	s = strings.ToLower(Bare(s))
	s = strings.Join(strings.Fields(s), " ")
	s = synonyms.Replace(fillers.Replace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("/.+-_~", r):
			b.WriteRune(r)
		case r < unicode.MaxASCII:
			b.WriteRune(' ')
		}
	}
	// A space survives only between two ASCII words, so "bot 简介" matches "bot简介".
	var out strings.Builder
	for _, w := range strings.Fields(b.String()) {
		w = strings.TrimRight(w, ".")
		if w == "" || englishFiller[w] {
			continue
		}
		if out.Len() > 0 {
			last, _ := utf8.DecodeLastRuneInString(out.String())
			first, _ := utf8.DecodeRuneInString(w)
			if last < unicode.MaxASCII && first < unicode.MaxASCII {
				out.WriteByte(' ')
			}
		}
		out.WriteString(w)
	}
	return out.String()
}

// anchors are the values a correction changes: digit runs, quoted text and paths.
func anchors(s string) map[string]bool {
	out := map[string]bool{}
	for _, q := range [][2]string{{"「", "」"}, {"“", "”"}, {"\"", "\""}, {"`", "`"}, {"『", "』"}} {
		rest := s
		for {
			i := strings.Index(rest, q[0])
			if i < 0 {
				break
			}
			rest = rest[i+len(q[0]):]
			j := strings.Index(rest, q[1])
			if j < 0 {
				break
			}
			out[strings.ToLower(strings.TrimSpace(rest[:j]))] = true
			rest = rest[j+len(q[1]):]
		}
	}
	for _, f := range strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("，。、；：,;()（）「」“”\"`『』", r)
	}) {
		if strings.Contains(f, "/") {
			out[strings.ToLower(strings.TrimRight(f, ".:"))] = true
		}
	}
	var digits strings.Builder
	flush := func() {
		if digits.Len() > 0 {
			out["#"+digits.String()] = true
			digits.Reset()
		}
	}
	for _, r := range s {
		if unicode.IsDigit(r) {
			digits.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

var englishFiller = map[string]bool{"a": true, "an": true, "the": true}
