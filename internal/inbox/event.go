package inbox

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"xbot/internal/tools"
)

const (
	NameTerminal = "terminal"
	NameTimer    = "timer"
	NameWeb      = "web"
	// NameRemoteOwner is the owner typing in a chat on another paired device.
	NameRemoteOwner = "remote-owner"
	// NameApproval carries the owner's late decision on a parked
	// confirmation card. Its sender is the owner who decided.
	NameApproval = "approval"
	NameCoding   = "coding-agent"

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
	// Images are pictures the sender attached, as file names in the bot's
	// workspace. The bytes are not in the event: they are already on disk.
	Images []string `json:"images,omitempty"`
	Audios []string `json:"audios,omitempty"`
	// RoutineSkills holds ordered library references for a timer wake, never bodies.
	RoutineSkills []string `json:"routine_skills,omitempty"`
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
	if (source != NameTerminal && source != NameWeb && source != NameRemoteOwner && source != NameApproval) || sender == "" {
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
