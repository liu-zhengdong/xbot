package peer

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/inbox"
	"xbot/internal/provider"
)

const (
	// KindBounce marks a row for a remote message that could not be delivered.
	KindBounce = "bounce"

	statusSent    = "sent"
	statusQueued  = "queued"
	statusFailed  = "failed"
	statusUnknown = "unknown"
)

// RemoteOutgoingFact is an ordinary internal transcript note for one remote attempt.
type RemoteOutgoingFact struct {
	Bot           string `json:"bot"`
	Text          string `json:"text"`
	Gateway       string `json:"gateway"`
	GatewayStatus int    `json:"gateway_status,omitempty"`
	Queued        bool   `json:"queued"`
	EnvelopeID    string `json:"envelope_id,omitempty"`
	Error         string `json:"error,omitempty"`
	CallID        string `json:"call_id"`
}

// RemoteOutgoingText serializes the complete fact for the source's existing recorder.
func RemoteOutgoingText(fact RemoteOutgoingFact) (string, error) {
	raw, err := json.Marshal(fact)
	if err != nil {
		return "", err
	}
	return "<peer_message_outgoing>\n" + string(raw) + "\n</peer_message_outgoing>", nil
}

// RemoteConversation reads the exchange between self and the bot addr on another
// paired device, for example writer@MacBook. Only this computer's half exists:
// incoming messages are the wrapped events written by remote.Receive, outgoing
// rows are internal remote facts or historical message_bot calls and tool results.
// Bounces and undelivered notices appear as KindBounce rows.
func RemoteConversation(a *bot.Bot, addr string) ([]Direct, error) {
	addr = strings.TrimSpace(addr)
	target, text, ok := strings.Cut(addr, "@")
	if a == nil || a.Name() == "" || !ok || !bot.ValidName(target) || text == "" {
		return nil, errors.New("peer: bad remote address")
	}
	results := map[string]string{}
	err := a.Walk(1, 0, func(_ int, e bot.Entry) error {
		if e.Message.Role == provider.RoleTool && e.Message.CallID() != "" {
			results[e.Message.CallID()] = e.Message.Text()
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var out []Direct
	err = a.Walk(1, 0, func(seq int, e bot.Entry) error {
		out = append(out, remoteRows(a.Name(), addr, seq, e, results)...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sortDirect(out)
	return out, nil
}

// remoteRows is the thread rows in one entry. One assistant entry can call
// message_bot to the same address several times; each call is its own row.
func remoteRows(self, addr string, seq int, e bot.Entry, results map[string]string) []Direct {
	at := e.Time.UTC()
	if e.Origin == bot.OriginInternal && e.Message.Role == provider.RoleUser && e.EventID == "" {
		text := e.Message.Text()
		const prefix, suffix = "<peer_message_outgoing>\n", "\n</peer_message_outgoing>"
		if !strings.HasPrefix(text, prefix) || !strings.HasSuffix(text, suffix) {
			return nil
		}
		var fact RemoteOutgoingFact
		raw := strings.TrimSuffix(strings.TrimPrefix(text, prefix), suffix)
		if !strings.HasPrefix(raw, "{") || json.Unmarshal([]byte(raw), &fact) != nil || fact.Bot != addr {
			return nil
		}
		status := statusFailed
		switch {
		case fact.Queued:
			status = statusQueued
		case fact.Gateway == "accepted":
			status = statusSent
		case fact.Gateway == "unknown":
			status = statusUnknown
		}
		return []Direct{{ID: fmt.Sprintf("peer_message_%d", seq), Seq: seq, From: self, To: addr, Text: fact.Text, Time: at, Status: status}}
	}
	if e.Message.Role == provider.RoleUser && e.EventID != "" {
		source, typ, sender, body, ok := unwrapEvent(e.Message.Text())
		if !ok {
			return nil
		}
		if source == "bot:"+addr && typ == inbox.TypeMessage && sender == addr {
			return []Direct{{ID: e.EventID, Seq: seq, From: addr, To: self, Text: body, Time: at}}
		}
		// remote.Receive and the outbox post "message to <addr> ..." notices as plain events.
		if (strings.HasPrefix(e.EventID, "remote_bounce_") || strings.HasPrefix(e.EventID, "remote_undelivered_")) &&
			source == "remote" && strings.HasPrefix(body, "message to "+addr+" ") {
			return []Direct{{ID: e.EventID, Seq: seq, From: addr, To: self, Text: body, Time: at, Kind: KindBounce}}
		}
		return nil
	}
	if e.Message.Role != provider.RoleAssistant {
		return nil
	}
	var out []Direct
	for _, tc := range e.Message.Calls() {
		if tc.Function.Name != "message_bot" || tc.ID == "" {
			continue
		}
		var args struct {
			Bot  string `json:"bot"`
			Text string `json:"text"`
		}
		if json.Unmarshal([]byte(tc.Function.Arguments), &args) != nil || strings.TrimSpace(args.Bot) != addr {
			continue
		}
		status := statusSent
		if result, called := results[tc.ID]; called {
			status = toolStatus(result)
		}
		out = append(out, Direct{ID: tc.ID, Seq: seq, From: self, To: addr, Text: args.Text, Time: at, Status: status})
	}
	return out
}

// toolStatus maps a message_bot result to the delivery state it reports.
func toolStatus(result string) string {
	switch {
	case strings.HasPrefix(result, "sent to"):
		return statusSent
	case strings.HasPrefix(result, "gateway unreachable"):
		return statusQueued
	}
	return statusFailed
}
