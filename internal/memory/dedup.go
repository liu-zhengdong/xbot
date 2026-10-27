package memory

import (
	"strings"
	"unicode"
)

// nearDupDice is the token-set similarity above which two facts say the same thing.
// "主人偏好简短回答" against "主人偏好详细回答" scores 0.57; a pure rewording scores 1.
const nearDupDice = 0.8

// minContainRunes keeps a short fragment such as "主人" from swallowing every fact.
const minContainRunes = 6

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
// provenance suffix, case, punctuation and common rewordings are removed, one
// contained in the other, or nearly the same token set. Numbers, quoted values and
// paths are anchors, and so is a negation: a fact that changes one of them is a
// correction, not a repeat.
func NearDuplicate(a, b string) bool {
	na, nb := normFact(a), normFact(b)
	if na == "" || nb == "" {
		return false
	}
	if na == nb {
		return true
	}
	if negated(na) != negated(nb) {
		return false
	}
	aa, ab := anchors(Bare(a)), anchors(Bare(b))
	short, long, as, al := na, nb, aa, ab
	if len([]rune(short)) > len([]rune(long)) {
		short, long, as, al = nb, na, ab, aa
	}
	if len([]rune(short)) >= minContainRunes && strings.Contains(long, short) && subset(as, al) {
		return true
	}
	if !subset(aa, ab) || !subset(ab, aa) {
		return false
	}
	return dice(tokens(na), tokens(nb)) >= nearDupDice
}

// normFact lowercases, folds rewordings and keeps letters, digits, a few path
// characters and single spaces between ASCII words.
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
	return strings.Join(strings.Fields(b.String()), " ")
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

// negated reports a negation word, so "no longer prefers X" does not repeat "prefers X".
func negated(norm string) bool {
	for _, w := range []string{"不", "没", "别", "勿", "禁止", "避免"} {
		if strings.Contains(norm, w) {
			return true
		}
	}
	for _, w := range strings.Fields(norm) {
		switch w {
		case "not", "no", "never", "don", "doesn", "isn", "avoid", "stop":
			return true
		}
	}
	return false
}

func subset(a, b map[string]bool) bool {
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// tokens are ASCII words plus bigrams of the other runes, so Chinese and
// English facts are compared at a similar grain.
func tokens(s string) map[string]bool {
	out := map[string]bool{}
	var word strings.Builder
	var prev rune
	for _, r := range s {
		if r < unicode.MaxASCII {
			if r == ' ' {
				if word.Len() > 0 && !englishFiller[word.String()] {
					out[word.String()] = true
				}
				word.Reset()
			} else {
				word.WriteRune(r)
			}
			prev = 0
			continue
		}
		if word.Len() > 0 && !englishFiller[word.String()] {
			out[word.String()] = true
		}
		word.Reset()
		if prev != 0 {
			out[string([]rune{prev, r})] = true
		}
		prev = r
	}
	if word.Len() > 0 && !englishFiller[word.String()] {
		out[word.String()] = true
	}
	return out
}

var englishFiller = map[string]bool{"a": true, "an": true, "the": true}

func dice(a, b map[string]bool) float64 {
	if len(a)+len(b) == 0 {
		return 0
	}
	n := 0
	for k := range a {
		if b[k] {
			n++
		}
	}
	return 2 * float64(n) / float64(len(a)+len(b))
}
