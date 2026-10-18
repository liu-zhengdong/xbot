package peer

import (
	"encoding/json"
	"errors"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/link"
	"xbot/internal/provider"
)

// ErrBadRemote is a remote name that is not "bot@device".
var ErrBadRemote = errors.New("peer: bad remote target")

// KindBounce marks a Direct that is a notice that a message did not arrive.
const KindBounce = "bounce"

// RemoteConversation reads self's exchange with a bot on a paired device,
// remote being "writer@MacBook". The other half lives on that device, so
// both directions come from self's transcript: what self sent from its
// message_bot calls, what it got from bot:writer@MacBook events, and bounces
// from the outbox's "was not delivered" notices (Kind KindBounce). Oldest
// first; sent rows carry To remote, so (To, Seq) stays a page cursor.
func RemoteConversation(self *bot.Bot, remote string) ([]Direct, error) {
	name, device, ok := strings.Cut(remote, "@")
	if self == nil || !ok || !bot.ValidName(name) || strings.TrimSpace(device) == "" {
		return nil, ErrBadRemote
	}
	me := self.Name()
	var out []Direct
	err := self.Walk(1, 0, func(seq int, e bot.Entry) error {
		at := e.Time.UTC()
		switch e.Message.Role {
		case provider.RoleAssistant:
			for _, c := range e.Message.ToolCalls {
				if c.Function.Name != "message_bot" {
					continue
				}
				var args struct{ Bot, To, Text string }
				if json.Unmarshal([]byte(c.Function.Arguments), &args) != nil {
					continue
				}
				if args.Bot == "" {
					args.Bot = args.To
				}
				if args.Bot == remote {
					out = append(out, Direct{ID: c.ID, Seq: seq, From: me, To: remote, Text: args.Text, Time: at})
				}
			}
		case provider.RoleUser:
			source, typ, sender, body, ok := unwrapEvent(e.Message.Content)
			if !ok || typ != link.TypeMessage {
				return nil
			}
			if source == "bot:"+remote && sender == remote {
				out = append(out, Direct{ID: e.EventID, Seq: seq, From: remote, To: me, Text: body, Time: at})
			} else if source == "remote" && strings.HasPrefix(body, "message to "+remote+" was not delivered") {
				out = append(out, Direct{ID: e.EventID, Seq: seq, From: remote, To: me, Text: body, Time: at, Kind: KindBounce})
			}
		}
		return nil
	})
	return out, err
}
