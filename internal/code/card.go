package code

import "strings"

// UseTranscript appends each coding card to the bot transcript.
// The text is already wrapped in <coding_card>.
func (l *Link) UseTranscript(fn func(string) error) {
	if l == nil {
		return
	}
	l.pathMu.Lock()
	defer l.pathMu.Unlock()
	l.transcript = fn
}

func (l *Link) output(id string) string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.tail[id]
}

func (l *Link) stream(id, status, text string) {
	if l == nil {
		return
	}
	text = strings.TrimSpace(text)
	l.pathMu.RLock()
	defer l.pathMu.RUnlock()
	l.mu.Lock()
	if l.tail == nil {
		l.tail = map[string]string{}
	}
	if text != "" {
		l.tail[id] = text
	}
	fn := l.transcript
	l.mu.Unlock()
	if fn == nil || text == "" {
		return
	}
	_ = fn(Card(id, status, text))
}

// Card is one transcript line for a coding task. The same id is rewritten by
// appending a newer card; the UI keeps the latest.
func Card(id, status, body string) string {
	body = strings.ReplaceAll(body, "</coding_card>", "< /coding_card>")
	return "<coding_card id=\"" + id + "\" status=\"" + status + "\">\n" + body + "\n</coding_card>"
}

// IsCard reports a coding-card transcript line. Those lines stay on screen and
// out of the model context.
func IsCard(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), "<coding_card ")
}
