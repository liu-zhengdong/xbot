package cliops

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// FormatPeerMessage is pure and includes the final newline used in both output
// budget checks and actual CLI printing. Receipts contain no message body.
func FormatPeerMessage(reply PeerMessageReply) string {
	var out bytes.Buffer
	if reply.Bot != "" {
		fmt.Fprintf(&out, "bot: %s\n", strconv.Quote(reply.Bot))
	}
	fmt.Fprintf(&out, "route: %s\nposted: %t\n", reply.Route, reply.Posted)
	if reply.Posted {
		out.WriteString("local receipt: recipient Inbox accepted; handling is unconfirmed\n")
	}
	if reply.EventID != "" {
		fmt.Fprintf(&out, "event_id: %s\n", reply.EventID)
	}
	if reply.EnvelopeID != "" {
		fmt.Fprintf(&out, "envelope_id: %s\n", reply.EnvelopeID)
	}
	if reply.Gateway != "" {
		fmt.Fprintf(&out, "gateway: %s\n", reply.Gateway)
		switch reply.Gateway {
		case "accepted":
			out.WriteString("gateway receipt: HTTP accepted and response read; recipient handling is unconfirmed\n")
		case "unknown":
			out.WriteString("gateway outcome unknown; acceptance may have occurred\n")
		}
	}
	if reply.GatewayStatus != 0 {
		fmt.Fprintf(&out, "gateway_status: %d\n", reply.GatewayStatus)
	}
	fmt.Fprintf(&out, "queued: %t\n", reply.Queued)
	if reply.Queued {
		out.WriteString("queue receipt: durably pending at source for retry, expires after 24h; recipient delivery is unconfirmed\n")
	}
	fmt.Fprintf(&out, "stage: %s\n", reply.Stage)
	if reply.Recorded != nil {
		fmt.Fprintf(&out, "recorded: %t\n", *reply.Recorded)
	}
	if reply.RecordSeq != 0 {
		fmt.Fprintf(&out, "record_seq: %d\n", reply.RecordSeq)
	}
	if reply.Error != "" {
		fmt.Fprintf(&out, "error: %s\n", strconv.Quote(reply.Error))
	}
	return out.String()
}

// boundPeerMessageReply only shortens display fields. It preserves every actual
// delivery and recording fact, including when a later display step fails.
func boundPeerMessageReply(reply PeerMessageReply) (PeerMessageReply, error) {
	fits := func(value PeerMessageReply) bool {
		raw, err := json.Marshal(value)
		return err == nil && len(raw)+1 <= peerMessageOutputBudget && len(FormatPeerMessage(value)) <= peerMessageOutputBudget
	}
	if fits(reply) {
		return reply, nil
	}
	problem := errors.New("peer-message: runtime receipt exceeds 32 KiB Shell output budget; preserve receipt before considering another send")
	reply.Error = problem.Error()
	if !fits(reply) {
		reply.Bot = ""
	}
	return reply, problem
}
