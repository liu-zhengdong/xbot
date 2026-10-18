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
//   - NewEvent marks content untrusted. The kernel, not the producer, decides
//     whether an owner on the terminal link is trusted.
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
	NameMemory   = "memory"
	NamePersona  = "persona"
	NameContext  = "context"
	NameAgents   = "agents"
	NameSkills   = "skills"
	NameRunner   = "runner"
	NameWebhook  = "webhook"
	NameWatch    = "watch"
	NameWeb      = "web"
	NamePeers    = "peers"
	// NameRemoteOwner is the owner typing in a chat on another paired device.
	NameRemoteOwner = "remote-owner"
	NameTelegram    = "telegram"
	NameLark        = "lark"
	NameCoding      = "coding-agent"
	NameDesktop     = "desktop"
	NameBrowser     = "browser"

	TypeMessage = "message"
	TypeTimer   = "timer"

	// KindPerson, KindBot, and KindGroup are the principal kinds on an event.
	KindPerson = "person"
	KindBot    = "bot"
	KindGroup  = "group"

	// SourceSubagent marks events a subagent posts back to the main inbox.
	// They are not terminal input, so the kernel wraps them as untrusted data.
	SourceSubagent = "subagent"
)

// Link is a channel between one bot and the outside world.
// Tools is the outbound direction. Inbound delivery is an Event posted to the inbox.
type Link interface {
	Name() string
	Tools() []tools.Tool
}

// Emitter is an inbound link that wakes the bot over time.
// The timer link implements Emitter.
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
	ID         string    `json:"id"`
	Source     string    `json:"source"`
	Type       string    `json:"type"`
	Content    string    `json:"content"`
	Sender     string    `json:"sender"`
	SenderKind string    `json:"sender_kind,omitempty"`
	Time       time.Time `json:"time"`
	Untrusted  bool      `json:"untrusted"`
	// Hops counts bot-to-bot deliveries in one chain. Zero is a person starting it.
	Hops  int    `json:"hops,omitempty"`
	Chain string `json:"chain,omitempty"`
	// ReplyTo is a transcript sequence the sender is quoting. Zero means none.
	ReplyTo int `json:"reply_to,omitempty"`
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
	ev := NewEvent(NameTerminal, TypeMessage, text, personID, at)
	ev.SenderKind = KindPerson
	return ev
}

// WebEvent is an owner line from the web link.
func WebEvent(personID, text string, at time.Time) Event {
	ev := NewEvent(NameWeb, TypeMessage, text, personID, at)
	ev.SenderKind = KindPerson
	return ev
}

// RemoteOwnerEvent is an owner line typed on another paired device.
// Only internal/remote creates this source, after it has checked that the
// envelope is authenticated by a paired device, that the device is bound to
// this person, that the person owns the bot, and that the body says the owner
// typed it. A bot relay, a tool, or message_bot never reaches it.
func RemoteOwnerEvent(personID, text string, at time.Time) Event {
	ev := NewEvent(NameRemoteOwner, TypeMessage, text, personID, at)
	ev.SenderKind = KindPerson
	return ev
}

// Validate checks the fields the kernel requires.
// Untrusted is not required: the kernel decides trust from the sender and the source.
func (e Event) Validate() error {
	if e.ID == "" || e.Source == "" || e.Type == "" || e.Sender == "" {
		return fmt.Errorf("event: id, source, type, and sender are required")
	}
	return nil
}

// OwnerTrusted is true for an owner speaking on the terminal, web, or
// remote-owner link. Any other source, including a timer wake or another bot,
// stays untrusted.
func OwnerTrusted(source, sender string, owners []string) bool {
	if (source != NameTerminal && source != NameWeb && source != NameRemoteOwner) || sender == "" {
		return false
	}
	for _, id := range owners {
		if id == sender {
			return true
		}
	}
	return false
}

// Render returns the event text the model should see.
// Trusted owner input is the raw text. Everything else is wrapped as data.
func Render(e Event, trusted bool) string {
	if trusted {
		return e.Content
	}
	return Wrap(e)
}

// UntrustedEventPrefix opens a wrapped event. Attributes follow the name, so
// callers must match this prefix and not the tag with an immediate ">".
const UntrustedEventPrefix = "<untrusted_event"

// Wrap renders an event as data the model must not treat as a system instruction.
// The content cannot close or forge the fence: its openers and closers are
// rewritten as in tools.NeutralizeUntrusted.
func Wrap(e Event) string {
	return fmt.Sprintf("%s source=%q type=%q sender=%q time=%q>\n%s\n</untrusted_event>",
		UntrustedEventPrefix, e.Source, e.Type, e.Sender, e.Time.UTC().Format(time.RFC3339), tools.NeutralizeUntrusted(e.Content))
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
