package cliops

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestPeerMessageRuntimeFormatReceiptFacts(t *testing.T) {
	for _, tc := range []struct {
		reply PeerMessageReply
		parts []string
	}{
		{PeerMessageReply{Bot: "B", Route: "local", Posted: true, EventID: "event-actual", Stage: "complete"}, []string{"posted: true\n", "event_id: event-actual\n", "Inbox accepted", "handling is unconfirmed", "queued: false\n"}},
		{PeerMessageReply{Route: "remote", EnvelopeID: "env-actual", Gateway: "accepted", Stage: "complete"}, []string{"gateway: accepted\n", "HTTP accepted and response read", "recipient handling is unconfirmed", "envelope_id: env-actual\n"}},
		{PeerMessageReply{Route: "remote", Gateway: "unknown", Queued: true, Stage: "complete"}, []string{"outcome unknown", "acceptance may have occurred", "queued: true\n", "durably pending at source", "24h"}},
		{PeerMessageReply{Route: "remote", Gateway: "http_error", GatewayStatus: 503, Stage: "queue", Error: "queue failed"}, []string{"gateway: http_error\n", "gateway_status: 503\n", "queued: false\n", "stage: queue\n", "queue failed"}},
		{PeerMessageReply{Route: "remote", Gateway: "not_attempted", Stage: "validation"}, []string{"gateway: not_attempted\n", "posted: false\n", "queued: false\n"}},
	} {
		got := FormatPeerMessage(tc.reply)
		for _, part := range tc.parts {
			if !strings.Contains(got, part) {
				t.Errorf("format=%q missing=%q", got, part)
			}
		}
		if !strings.HasSuffix(got, "\n") || strings.HasSuffix(got, "\n\n") || strings.Contains(got, "recorded:") {
			t.Fatalf("format newline/local recorded=%q", got)
		}
	}
	for _, recorded := range []bool{true, false} {
		reply := PeerMessageReply{Route: "remote", Gateway: "accepted", Recorded: &recorded, RecordSeq: 42, Stage: "record"}
		text := FormatPeerMessage(reply)
		raw, _ := json.Marshal(reply)
		if !strings.Contains(text, "recorded:") || !strings.Contains(text, "record_seq: 42\n") || !strings.Contains(string(raw), `"recorded":`) {
			t.Fatalf("recorded=%t text=%q raw=%s", recorded, text, raw)
		}
	}
}

func TestPeerMessageRuntimeDisplayBudgetPreservesFacts(t *testing.T) {
	recorded := false
	actual := PeerMessageReply{Bot: strings.Repeat("\x00", 12<<10), Route: "remote", Posted: true, EventID: "event-actual", EnvelopeID: "env-actual", Gateway: "unknown", GatewayStatus: 503, Queued: true, Stage: "record", Recorded: &recorded, RecordSeq: 123, Error: strings.Repeat("\x01", 12<<10)}
	bounded, err := boundPeerMessageReply(actual)
	if err == nil || bounded.Error == "" || bounded.Bot != "" {
		t.Fatalf("not bounded: reply=%+v err=%v", bounded, err)
	}
	wantFacts := actual
	wantFacts.Bot, wantFacts.Error = bounded.Bot, bounded.Error
	if !reflect.DeepEqual(bounded, wantFacts) {
		t.Fatalf("facts changed: got=%+v want=%+v", bounded, wantFacts)
	}
	raw, err := json.Marshal(bounded)
	if err != nil || len(raw)+1 > 32<<10 || len(FormatPeerMessage(bounded)) > 32<<10 {
		t.Fatalf("raw=%d human=%d err=%v", len(raw)+1, len(FormatPeerMessage(bounded)), err)
	}
	ordinary := PeerMessageReply{Bot: "B", Route: "local", Posted: true, EventID: "actual", Stage: "complete"}
	if got, err := boundPeerMessageReply(ordinary); err != nil || !reflect.DeepEqual(got, ordinary) {
		t.Fatalf("ordinary receipt changed: %+v %v", got, err)
	}
}
