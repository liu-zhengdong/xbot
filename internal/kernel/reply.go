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
	_, err := k.recordTurn(k.curTurn(), m)
	return err
}

// recordTurn appends m for turn t, on t's branch once t has moved to the background.
// The returned sequence is the line just written.
func (k *Kernel) recordTurn(t *turn, m provider.Message) (int, error) {
	replyTo := m.ReplyTo
	m.ReplyTo = 0
	origin := bot.OriginOwner
	if m.Internal {
		origin = bot.OriginInternal
	}
	m.Internal = false
	if m.Role == provider.RoleUser && replyTo > 0 {
		if stripped, ok := bot.StripInjectedQuote(m.Content); ok {
			m.Content = stripped
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if m.Role == provider.RoleAssistant {
		t.replies++
		if len(m.ToolCalls) == 0 {
			t.replyTo = replyTo
			t.answered = true
		}
	}
	// A user line answered inside a running turn carries its pictures with it;
	// the names are recorded on the line so a later turn can find them.
	images := m.ImageNames
	m.ImageNames = nil
	// A note naming the pictures may have been added to reach the model in this
	// request. The line stores the owner's words; the note comes back when the
	// line is assembled.
	if len(images) > 0 {
		m.Content = stripPastedNote(m.Content, k.pastedNote(images))
	}
	seq, err := k.Bot.AppendSeq(bot.Entry{Time: k.now(), Message: k.scrubEntry(m), ReplyTo: replyTo, Branch: t.branch, Origin: origin, Images: images})
	if err != nil {
		return 0, err
	}
	t.lastRec = seq
	return seq, nil
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
// Environment updates, memory blocks, and any other kernel note are not user
// messages. The origin field on the transcript record decides, not the text.
func (k *Kernel) userReplyTarget(seq int) bool {
	if seq < 1 || k == nil || k.Bot == nil {
		return false
	}
	part, err := k.Bot.Slice(seq, seq)
	if err != nil || len(part) != 1 {
		return false
	}
	e := part[0]
	return e.Message.Role == provider.RoleUser && !e.IsInternal()
}

// replyProvider strips a leading reply_to line from assistant text before it
// is stored or shown. The sequence stays on Message.ReplyTo.
type replyProvider struct {
	inner  provider.Provider
	accept func(int) bool
	// onSend runs before the request goes out, so a call that fails or is
	// cancelled still left the context it was about to send.
	onSend func(provider.Request)
	onUse  func(provider.Request, provider.Message)
	hold   replyHold
}

func (s *replyProvider) Chat(ctx context.Context, req provider.Request, onDelta func(string)) (provider.Message, error) {
	s.hold = replyHold{}
	if s.onSend != nil {
		s.onSend(req)
	}
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
	if s.onUse != nil {
		s.onUse(req, msg)
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
