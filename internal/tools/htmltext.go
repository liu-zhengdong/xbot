package tools

import (
	"html"
	"strings"
	"unicode"
)

// HTMLToText turns a page into readable text. Script and style bodies are
// dropped. Links keep their target. The scan is linear in the input size.
func HTMLToText(src string) string {
	var b strings.Builder
	b.Grow(len(src) / 2)
	i := 0
	lower := strings.ToLower(src)
	var openHref string
	for i < len(src) {
		if src[i] != '<' {
			b.WriteByte(src[i])
			i++
			continue
		}
		end := strings.IndexByte(src[i:], '>')
		if end < 0 {
			b.WriteString(src[i:])
			break
		}
		tag := src[i+1 : i+end]
		i += end + 1
		name, href, closing := tagInfo(tag)
		switch name {
		case "script", "style", "noscript":
			if !closing {
				closeTag := "</" + name
				j := strings.Index(lower[i:], closeTag)
				if j < 0 {
					return collapseSpace(b.String())
				}
				i += j
			}
		case "br", "p", "div", "h1", "h2", "h3", "h4", "h5", "h6", "li", "tr", "section", "article":
			b.WriteByte('\n')
		case "a":
			if closing {
				if openHref != "" {
					b.WriteString(" (")
					b.WriteString(openHref)
					b.WriteByte(')')
				}
				openHref = ""
			} else if href != "" {
				openHref = href
			}
		}
	}
	return collapseSpace(html.UnescapeString(b.String()))
}

func tagInfo(tag string) (name, href string, closing bool) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return "", "", false
	}
	if tag[0] == '/' {
		closing = true
		tag = strings.TrimSpace(tag[1:])
	}
	if tag != "" && tag[len(tag)-1] == '/' {
		tag = strings.TrimSpace(tag[:len(tag)-1])
	}
	nameEnd := 0
	for nameEnd < len(tag) && !unicode.IsSpace(rune(tag[nameEnd])) {
		nameEnd++
	}
	name = strings.ToLower(tag[:nameEnd])
	if name == "a" && !closing {
		href = attr(tag[nameEnd:], "href")
	}
	return name, href, closing
}

func attr(tag, key string) string {
	lower := strings.ToLower(tag)
	needle := key + "="
	i := strings.Index(lower, needle)
	if i < 0 {
		return ""
	}
	rest := strings.TrimSpace(tag[i+len(needle):])
	if rest == "" {
		return ""
	}
	if rest[0] == '"' || rest[0] == '\'' {
		q := rest[0]
		rest = rest[1:]
		j := strings.IndexByte(rest, q)
		if j < 0 {
			return rest
		}
		return rest[:j]
	}
	j := strings.IndexAny(rest, " \t\r\n>")
	if j < 0 {
		return rest
	}
	return rest[:j]
}

func collapseSpace(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	blank := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			blank++
			if blank <= 1 && len(out) > 0 {
				out = append(out, "")
			}
			continue
		}
		blank = 0
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// applyFocus keeps paragraphs that contain focus. No match returns a short
// note and the start of the page, so a miss does not hide the document.
func applyFocus(text, focus string) string {
	focus = strings.TrimSpace(focus)
	if focus == "" {
		return text
	}
	needle := strings.ToLower(focus)
	var keep []string
	for _, para := range strings.Split(text, "\n\n") {
		if strings.Contains(strings.ToLower(para), needle) {
			keep = append(keep, para)
		}
	}
	if len(keep) == 0 {
		head := text
		if len(head) > 1024 {
			head = head[:1024]
		}
		return "focus did not match\n\n" + head
	}
	return strings.Join(keep, "\n\n")
}
