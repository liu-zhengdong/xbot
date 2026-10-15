package bot

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// QuoteRunes is the quote length injected into a turn. The transcript keeps the full line.
const QuoteRunes = 300

const quoteLead = "user is replying to your earlier message (seq "

// FormatQuote is the one line prepended to a user message that quotes seq.
// text is cut to QuoteRunes. A cut quote ends with ….
func FormatQuote(seq int, text string) string {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > QuoteRunes {
		text = string([]rune(text)[:QuoteRunes]) + "…"
	}
	return fmt.Sprintf("%s%d): %s", quoteLead, seq, text)
}

// StripInjectedQuote removes a leading FormatQuote line and the blank line after it.
// The stored transcript keeps the owner's words, not the injected quote.
func StripInjectedQuote(content string) (string, bool) {
	if !strings.HasPrefix(content, quoteLead) {
		return content, false
	}
	idx := strings.Index(content, "\n\n")
	if idx < 0 {
		return content, false
	}
	return content[idx+2:], true
}

// TakeReplyLine removes a leading "reply_to: <seq>" line from a model reply.
// ok is false when that line is absent. seq is positive when ok is true.
func TakeReplyLine(content string) (seq int, rest string, ok bool) {
	line, rest, found := strings.Cut(content, "\n")
	seq, ok = parseReplyLine(line)
	if !ok {
		return 0, content, false
	}
	if !found {
		rest = ""
	}
	rest = strings.TrimLeft(rest, " \t")
	return seq, rest, true
}

func parseReplyLine(line string) (int, bool) {
	line = strings.TrimSpace(line)
	const marker = "reply_to:"
	if !strings.HasPrefix(line, marker) {
		return 0, false
	}
	raw := strings.TrimSpace(strings.TrimPrefix(line, marker))
	if raw == "" {
		return 0, false
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}
