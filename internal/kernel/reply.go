package kernel

import (
	"context"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/provider"
)

// ReplySeq is the user-message sequence the latest assistant reply pointed at.
// Zero means this turn's reply did not quote one.
func (k *Kernel) ReplySeq() int {
	if k == nil {
		return 0
	}
	t := k.curTurn()
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.replyTo
}

func (k *Kernel) record(m provider.Message) error {
	return k.recordTurn(k.curTurn(), m)
}

// recordTurn appends m for turn t, on t's branch once t has moved to the background.
func (k *Kernel) recordTurn(t *turn, m provider.Message) error {
	replyTo := m.ReplyTo
	m.ReplyTo = 0
	if m.Role == provider.RoleUser && replyTo > 0 {
		if stripped, ok := bot.StripInjectedQuote(m.Content); ok {
			m.Content = stripped
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if m.Role == provider.RoleAssistant {
		t.steps++
		if len(m.ToolCalls) == 0 {
			t.replyTo = replyTo
			t.answered = true
		}
	}
	return k.Bot.Append(bot.Entry{Time: k.now(), Message: k.scrubEntry(m), ReplyTo: replyTo, Branch: t.branch})
}

func (k *Kernel) quotePrefix(seq int) string {
	if seq < 1 || k == nil || k.Bot == nil {
		return ""
	}
	part, err := k.Bot.Slice(seq, seq)
	if err != nil || len(part) != 1 {
		return ""
	}
	return bot.FormatQuote(seq, part[0].Message.Content)
}

// userReplyTarget reports whether seq is a user utterance the bot may point at.
// Environment updates and memory blocks are not user messages.
func (k *Kernel) userReplyTarget(seq int) bool {
	if seq < 1 || k == nil || k.Bot == nil {
		return false
	}
	part, err := k.Bot.Slice(seq, seq)
	if err != nil || len(part) != 1 {
		return false
	}
	e := part[0]
	if e.Message.Role != provider.RoleUser {
		return false
	}
	c := strings.TrimSpace(e.Message.Content)
	switch {
	case strings.HasPrefix(c, "<environment_"), strings.HasPrefix(c, "<memory>"), strings.HasPrefix(c, "<summary "):
		return false
	default:
		return true
	}
}

// replyProvider strips a leading reply_to line from assistant text before it
// is stored or shown. The sequence stays on Message.ReplyTo.
type replyProvider struct {
	inner  provider.Provider
	accept func(int) bool
	hold   replyHold
}

func (s *replyProvider) Chat(ctx context.Context, req provider.Request, onDelta func(string)) (provider.Message, error) {
	s.hold = replyHold{}
	msg, err := s.inner.Chat(ctx, req, func(chunk string) {
		if onDelta == nil {
			return
		}
		if out := s.hold.Push(chunk); out != "" {
			onDelta(out)
		}
	})
	if err != nil {
		return provider.Message{}, err
	}
	if onDelta != nil {
		if rest := s.hold.Flush(); rest != "" {
			onDelta(rest)
		}
	}
	if msg.Role == "" {
		msg.Role = provider.RoleAssistant
	}
	if len(msg.ToolCalls) == 0 {
		if seq, rest, ok := bot.TakeReplyLine(msg.Content); ok {
			msg.Content = rest
			if s.accept != nil && s.accept(seq) {
				msg.ReplyTo = seq
			}
		}
	}
	return msg, nil
}

// replyHold keeps a leading "reply_to:" line out of the live text stream.
type replyHold struct {
	buf  strings.Builder
	done bool
}

func (h *replyHold) Push(s string) string {
	if h.done {
		return s
	}
	h.buf.WriteString(s)
	cur := h.buf.String()
	if !strings.Contains(cur, "\n") && pendingReply(cur) {
		return ""
	}
	h.done = true
	h.buf.Reset()
	if _, rest, ok := bot.TakeReplyLine(cur); ok {
		return rest
	}
	return cur
}

func (h *replyHold) Flush() string {
	if h.done {
		return ""
	}
	h.done = true
	cur := h.buf.String()
	h.buf.Reset()
	if _, rest, ok := bot.TakeReplyLine(cur); ok {
		return rest
	}
	return cur
}

func pendingReply(s string) bool {
	t := strings.TrimLeft(s, " \t")
	const marker = "reply_to:"
	if strings.HasPrefix(marker, t) {
		return true
	}
	if !strings.HasPrefix(t, marker) {
		return false
	}
	rest := strings.TrimLeft(strings.TrimPrefix(t, marker), " \t")
	for _, r := range rest {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
