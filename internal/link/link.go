// Package link is the only way a bot touches the outside world.
//
// A Link has two directions:
//   - Outbound: Tools the model may call (shell, files, and later Slack or HTTP).
//   - Inbound: external happenings become Events posted to the bot's inbox.
//
// The kernel knows the inbox and the tools. It does not know Slack, MCP, or the terminal.
// Proactivity is a timer Link that emits events. M1 defines Emitter only; cron is M2.
//
// Kernel rules enforced here and in the kernel:
//   - Event content is untrusted data. NewEvent always sets Untrusted.
//   - Messages sent on the user's behalf stay drafts until confirmed (SendPolicy).
//   - Which links exist is a per-bot grant in the bot config, not a global switch.
package link

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"xbot/internal/tools"
)

const (
	NameTerminal = "terminal"
	NameTools    = "tools"
	NameTimer    = "timer"

	TypeMessage = "message"
	TypeTimer   = "timer"
)

// Link is a channel between one bot and the outside world.
// Tools is the outbound direction. Inbound delivery is an Event posted to the inbox.
type Link interface {
	Name() string
	Tools() []tools.Tool
}

// Emitter is an inbound link that wakes the bot over time.
// A timer link implements Emitter. M1 does not schedule anything.
type Emitter interface {
	Link
	Run(ctx context.Context, emit func(Event) error) error
}

// Messenger sends a message on the user's behalf.
// No M1 link implements it. The kernel calls Draft, then SendDraft only after confirmation.
type Messenger interface {
	Link
	Draft(ctx context.Context, to, body string) (preview string, err error)
	SendDraft(ctx context.Context, to, body string) error
}

// SendPolicy is the kernel rule for outbound messages on the user's behalf.
// The zero value keeps the message as a draft until someone approves it.
type SendPolicy struct {
	// AutoSend skips confirmation. Leave it false.
	AutoSend bool
}

// Allowed reports whether a draft may be sent.
func (p SendPolicy) Allowed(approved bool) bool {
	return p.AutoSend || approved
}

// ErrNotSent means the message remains a draft.
var ErrNotSent = errNotSent{}

type errNotSent struct{}

func (errNotSent) Error() string { return "link: outbound message kept as draft" }

// Event is one inbound happening. Content is data, not a system instruction.
type Event struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"`
	Type      string    `json:"type"`
	Content   string    `json:"content"`
	Sender    string    `json:"sender"`
	Time      time.Time `json:"time"`
	Untrusted bool      `json:"untrusted"`
}

// NewEvent builds an event. Untrusted is always true.
func NewEvent(source, typ, content, sender string, at time.Time) Event {
	if at.IsZero() {
		at = time.Now()
	}
	return Event{
		ID:        newID(),
		Source:    source,
		Type:      typ,
		Content:   content,
		Sender:    sender,
		Time:      at.UTC(),
		Untrusted: true,
	}
}

// TerminalEvent is a user line from the terminal link.
func TerminalEvent(personID, text string, at time.Time) Event {
	return NewEvent(NameTerminal, TypeMessage, text, personID, at)
}

// Validate checks the fields the kernel requires.
func (e Event) Validate() error {
	if e.ID == "" || e.Source == "" || e.Type == "" || e.Sender == "" {
		return fmt.Errorf("event: id, source, type, and sender are required")
	}
	if !e.Untrusted {
		return fmt.Errorf("event: content must be untrusted")
	}
	return nil
}

// Wrap renders an event as data the model must not treat as a system instruction.
func Wrap(e Event) string {
	return fmt.Sprintf("<untrusted_event source=%q type=%q sender=%q time=%q>\n%s\n</untrusted_event>",
		e.Source, e.Type, e.Sender, e.Time.UTC().Format(time.RFC3339), e.Content)
}

func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("ev_%d", time.Now().UnixNano())
	}
	return "ev_" + hex.EncodeToString(b[:])
}

// CollectTools returns tools from the granted links, in grant order.
func CollectTools(links []Link) []tools.Tool {
	var out []tools.Tool
	for _, l := range links {
		out = append(out, l.Tools()...)
	}
	return out
}
