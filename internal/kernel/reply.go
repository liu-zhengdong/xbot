package kernel

import (
	"context"
	"encoding/json"
	"errors"
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
	if m.Seq > 0 {
		t.mu.Lock()
		t.lastRec = m.Seq
		t.mu.Unlock()
		return m.Seq, nil
	}

	replyTo := m.ReplyTo
	m.ReplyTo = 0
	origin := bot.OriginOwner
	if m.Internal {
		origin = bot.OriginInternal
	}
	m.Internal = false
	if m.Role == provider.RoleUser && replyTo > 0 {
		if stripped, ok := bot.StripInjectedQuote(m.Text()); ok {
			m.SetText(stripped)
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if m.Role == provider.RoleAssistant {
		t.replies++
		if len(m.Calls()) == 0 {
			t.replyTo = replyTo
			t.answered = true
		}
	}
	// A user line answered inside a running turn carries its pictures with it;
	// the names are recorded on the line so a later turn can find them.
	images := m.ImageNames
	m.ImageNames = nil
	// The final reply carries the pictures notify_owner queued, so the chat
	// shows them in that bubble.
	if m.Role == provider.RoleAssistant && len(m.Calls()) == 0 && len(t.outImages) > 0 {
		images = append(images, t.takeOutImagesLocked()...)
	}
	// A note naming the pictures may have been added to reach the model in this
	// request. The line stores the owner's words; the note comes back when the
	// line is assembled.
	if len(images) > 0 && m.Role == provider.RoleUser {
		m.SetText(stripPastedNote(m.Text(), k.pastedNote(images)))
	}
	seq, err := k.Bot.AppendSeq(bot.Entry{Time: k.now(), EventID: m.EventID, Message: k.scrubEntry(m), ReplyTo: replyTo, Branch: t.branch, Origin: origin, Images: images})
	if err != nil {
		return 0, err
	}
	if m.Role == provider.RoleAssistant && len(m.Calls()) == 0 {
		t.replyGen++
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
	return bot.FormatQuote(seq, part[0].Message.Text())
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
	inner        provider.Provider
	schemaKernel *Kernel
	accept       func(int) bool
	// onSend runs before the request goes out, so a call that fails or is
	// cancelled still left the context it was about to send.
	prepare  func(provider.Request) (provider.Request, error)
	finish   func(context.Context, provider.Request, provider.Request) (provider.Request, error)
	onSend   func(provider.Request)
	onUse    func(provider.Request, provider.Message)
	onEnd    func(provider.Request, provider.Message, error)
	validate func(provider.Request, provider.Message) error
	// fit cuts a request to stay under the context lines (see fit.go).
	// overflow says the provider rejected it as too long; the cut request is
	// then sent once more.
	fit func(ctx context.Context, req provider.Request, overflow bool) provider.Request
}

func (s *replyProvider) send(ctx context.Context, req provider.Request, onDelta func(string)) (provider.Message, error) {
	if s.onSend != nil {
		s.onSend(req)
	}
	msg, err := s.inner.Chat(ctx, req, onDelta)
	if s.onEnd != nil {
		s.onEnd(req, msg, err)
	}
	return msg, err
}

// replyRequest keeps preparation, fitting and accounting per physical request.
// Reply validation belongs to the assembled logical completion below.
type replyRequest struct{ owner *replyProvider }

func (p replyRequest) Chat(ctx context.Context, req provider.Request, onDelta func(string)) (provider.Message, error) {
	s := p.owner
	if s.prepare != nil {
		var err error
		req, err = s.prepare(req)
		if err != nil {
			return provider.Message{}, err
		}
	}
	raw := req
	if s.fit != nil {
		req = s.fit(ctx, raw, false)
	}
	if s.finish != nil {
		var err error
		req, err = s.finish(ctx, raw, req)
		if err != nil {
			return provider.Message{}, err
		}
	}
	schemaAttempt := &mcpSchemaAttempt{k: s.schemaKernel}
	req, msg, err := schemaAttempt.chat(ctx, req, onDelta, s.send)
	if err != nil && s.fit != nil && provider.IsContextOverflow(err) {
		req = s.fit(ctx, raw, true)
		if s.finish != nil {
			req, err = s.finish(ctx, raw, req)
			if err != nil {
				return provider.Message{}, err
			}
		}
		req, msg, err = schemaAttempt.chat(ctx, req, onDelta, s.send)
	}
	if err != nil {
		return provider.Message{}, err
	}
	if s.onUse != nil {
		s.onUse(req, msg)
	}
	return msg, nil
}

func (s *replyProvider) Chat(ctx context.Context, req provider.Request, onDelta func(string)) (provider.Message, error) {
	msg, err := provider.Complete(ctx, replyRequest{s}, req, nil)
	if err != nil {
		return provider.Message{}, err
	}
	if msg.Role == "" {
		msg.Role = provider.RoleAssistant
	}
	if len(msg.Calls()) == 0 {
		if seq, rest, ok := bot.TakeReplyLine(msg.Text()); ok {
			msg.SetText(rest)
			if s.accept != nil && s.accept(seq) {
				msg.ReplyTo = seq
			}
		}
	}
	if s.validate != nil {
		if err := s.validate(req, msg); err != nil {
			return provider.Message{}, err
		}
	}
	if len(msg.Calls()) == 0 && toolProtocolText(msg.Text()) {
		return provider.Message{}, errors.New("provider returned tool protocol as reply text")
	}
	if onDelta != nil && msg.Text() != "" {
		onDelta(msg.Text())
	}
	return msg, nil
}

// A provider protocol packet is not a normal assistant answer. Mentions and
// fenced examples remain ordinary text, including explanations of DSML.
func toolProtocolText(text string) bool {
	if _, rest, ok := bot.TakeReplyLine(text); ok {
		text = rest
	}
	text = strings.TrimSpace(text)
	if (strings.HasPrefix(text, "<｜DSML｜") || strings.HasPrefix(text, "<｜｜DSML｜")) && strings.Contains(text, "invoke") {
		return true
	}
	var raw struct {
		Tools []json.RawMessage `json:"tool_calls"`
	}
	return json.Unmarshal([]byte(text), &raw) == nil && len(raw.Tools) > 0
}
