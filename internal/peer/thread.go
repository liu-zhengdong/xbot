package peer

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
)

// Direct is one private bot-to-bot delivery recorded in the recipient transcript.
// Seq is that transcript's sequence, the same number export and expand use.
// Kind is "" for a message and "bounce" for an undelivered remote message.
// Status is set only on outgoing remote rows: sent, queued, or failed.
type Direct struct {
	ID     string
	Seq    int
	From   string
	To     string
	Text   string
	Time   time.Time
	Kind   string
	Status string
}

// Conversation reads the private messages between a and b.
// Each delivery is stored on the recipient. Group posts, the sender's tool call,
// and inbox events the recipient has not recorded yet are left out.
func Conversation(a, b *bot.Bot) ([]Direct, error) {
	if a == nil || b == nil || a.Name() == "" || b.Name() == "" || a.Name() == b.Name() {
		return nil, errors.New("peer: bad target")
	}
	var out []Direct
	if err := collectDirect(a, b.Name(), &out); err != nil {
		return nil, err
	}
	if err := collectDirect(b, a.Name(), &out); err != nil {
		return nil, err
	}
	sortDirect(out)
	return out, nil
}

// sortDirect orders a thread by time, then recipient, seq, and id.
func sortDirect(out []Direct) {
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Time.Equal(out[j].Time) {
			return out[i].Time.Before(out[j].Time)
		}
		if out[i].To != out[j].To {
			return out[i].To < out[j].To
		}
		if out[i].Seq != out[j].Seq {
			return out[i].Seq < out[j].Seq
		}
		return out[i].ID < out[j].ID
	})
}

func collectDirect(self *bot.Bot, peerName string, out *[]Direct) error {
	return self.Walk(1, 0, func(seq int, e bot.Entry) error {
		if e.Message.Role != provider.RoleUser || e.EventID == "" {
			return nil
		}
		from, text, ok := directText(e.Message.Content, peerName)
		if !ok {
			return nil
		}
		*out = append(*out, Direct{
			ID:   e.EventID,
			Seq:  seq,
			From: from,
			To:   self.Name(),
			Text: text,
			Time: e.Time.UTC(),
		})
		return nil
	})
}

func directText(content, peerName string) (from, text string, ok bool) {
	source, typ, sender, body, ok := unwrapEvent(content)
	if !ok || source != "bot:"+peerName || typ != link.TypeMessage || sender != peerName {
		return "", "", false
	}
	return sender, body, true
}

// unwrapEvent reverses link.Wrap. The body is the raw event content.
func unwrapEvent(content string) (source, typ, sender, body string, ok bool) {
	const prefix = "<untrusted_event "
	const suffix = "\n</untrusted_event>"
	if !strings.HasPrefix(content, prefix) || !strings.HasSuffix(content, suffix) {
		return
	}
	inner := content[len(prefix) : len(content)-len(suffix)]
	nl := strings.IndexByte(inner, '\n')
	if nl < 1 || inner[nl-1] != '>' {
		return
	}
	attrs, ok := parseAttrs(inner[:nl-1])
	if !ok {
		return "", "", "", "", false
	}
	source, typ, sender = attrs["source"], attrs["type"], attrs["sender"]
	if source == "" || typ == "" || sender == "" {
		return "", "", "", "", false
	}
	return source, typ, sender, inner[nl+1:], true
}

func parseAttrs(s string) (map[string]string, bool) {
	out := map[string]string{}
	for {
		s = strings.TrimLeft(s, " ")
		if s == "" {
			return out, true
		}
		eq := strings.IndexByte(s, '=')
		if eq <= 0 {
			return nil, false
		}
		key := s[:eq]
		quoted, err := strconv.QuotedPrefix(s[eq+1:])
		if err != nil {
			return nil, false
		}
		val, err := strconv.Unquote(quoted)
		if err != nil {
			return nil, false
		}
		out[key] = val
		s = s[eq+1+len(quoted):]
	}
}
