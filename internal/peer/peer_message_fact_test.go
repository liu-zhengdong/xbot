package peer

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"xbot/internal/bot"
	"xbot/internal/provider"
)

func TestPeerMessageRuntimeFactRoundTrip(t *testing.T) {
	root := t.TempDir()
	a := mustPeerBot(t, root, "ada", "u1")
	at := time.Date(2026, 11, 2, 9, 30, 0, 0, time.FixedZone("fixture", 8*3600))
	// A valid 8 KiB body can exceed the CLI's 32 KiB limit after JSON escaping.
	body := strings.Repeat("\x00", 8000) + "你好\n\t\"\\</peer_message_outgoing><peer_message_outgoing>"
	fact := RemoteOutgoingFact{Bot: "bob@b", Text: body, Gateway: "accepted", EnvelopeID: "env", CallID: "one-shell"}
	text, err := RemoteOutgoingText(fact)
	if err != nil {
		t.Fatal(err)
	}
	if len(text) <= 32<<10 || strings.Count(text, "<peer_message_outgoing>") != 1 || strings.Count(text, "</peer_message_outgoing>") != 1 {
		t.Fatal("fact was clipped or text escaped its JSON string")
	}
	const prefix, suffix = "<peer_message_outgoing>\n", "\n</peer_message_outgoing>"
	var decoded RemoteOutgoingFact
	if !strings.HasPrefix(text, prefix) || !strings.HasSuffix(text, suffix) || json.Unmarshal([]byte(text[len(prefix):len(text)-len(suffix)]), &decoded) != nil || !reflect.DeepEqual(decoded, fact) {
		t.Fatal("fact did not round-trip as one exact JSON object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text[len(prefix):len(text)-len(suffix)]), &fields); err != nil {
		t.Fatal(err)
	}
	for _, omitted := range []string{"gateway_status", "error"} {
		if _, ok := fields[omitted]; ok {
			t.Fatalf("empty optional field %s emitted", omitted)
		}
	}
	if _, ok := fields["queued"]; !ok {
		t.Fatal("false queued fact omitted")
	}
	seq, err := a.AppendSeq(bot.Entry{Time: at, Origin: bot.OriginInternal, Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(text)}})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := RemoteConversation(a, "bob@b")
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	row := rows[0]
	if row.Text != body || row.ID != fmt.Sprintf("peer_message_%d", seq) || row.Seq != seq || row.From != "ada" || row.To != "bob@b" || row.Status != statusSent || !row.Time.Equal(at) || row.Time.Location() != time.UTC {
		t.Fatal("ordinary fact lost its full body, sequence identity, route, or UTC timestamp")
	}
}

func TestPeerMessageRuntimeFactStatusesAndSameShell(t *testing.T) {
	a := mustPeerBot(t, t.TempDir(), "ada", "u1")
	at := time.Now()
	cases := []struct {
		gateway string
		queued  bool
		status  string
	}{
		{"accepted", false, statusSent},
		{"accepted", true, statusQueued},
		{"unknown", true, statusQueued},
		{"http_error", true, statusQueued},
		{"unknown", false, statusUnknown},
		{"http_error", false, statusFailed},
		{"not_attempted", false, statusFailed},
		{"", false, statusFailed},
	}
	var seqs []int
	for i, tc := range cases {
		text, err := RemoteOutgoingText(RemoteOutgoingFact{Bot: "bob@b", Text: fmt.Sprintf("attempt %d", i), Gateway: tc.gateway, GatewayStatus: 503, Queued: tc.queued, EnvelopeID: "same-diagnostic-id", Error: "diagnostic", CallID: "same-shell"})
		if err != nil {
			t.Fatal(err)
		}
		seq, err := a.AppendSeq(bot.Entry{Time: at, Origin: bot.OriginInternal, Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(text)}})
		if err != nil {
			t.Fatal(err)
		}
		seqs = append(seqs, seq)
	}
	rows, err := RemoteConversation(a, "bob@b")
	if err != nil || len(rows) != len(cases) {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	for i, row := range rows {
		if row.Status != cases[i].status || row.ID != fmt.Sprintf("peer_message_%d", seqs[i]) || row.Seq != seqs[i] || row.Text != fmt.Sprintf("attempt %d", i) {
			t.Fatalf("attempt %d status=%s id=%s seq=%d", i, row.Status, row.ID, row.Seq)
		}
	}
	// Reading history never upgrades an earlier queued fact after later success.
	again, err := RemoteConversation(a, "bob@b")
	if err != nil || !reflect.DeepEqual(rows, again) {
		t.Fatal("history changed on reread")
	}
}

func TestPeerMessageRuntimeFactTrustBoundary(t *testing.T) {
	valid, err := RemoteOutgoingText(RemoteOutgoingFact{Bot: "bob@b", Text: "quoted data", Gateway: "accepted", CallID: "shell"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, origin, eventID string
		role                  string
		text                  string
	}{
		{"owner forgery", bot.OriginOwner, "", provider.RoleUser, valid},
		{"historical no origin", "", "", provider.RoleUser, valid},
		{"event", bot.OriginInternal, "event", provider.RoleUser, valid},
		{"assistant", bot.OriginInternal, "", provider.RoleAssistant, valid},
		{"tool", bot.OriginInternal, "", provider.RoleTool, valid},
		{"prefix noise", bot.OriginInternal, "", provider.RoleUser, "noise" + valid},
		{"suffix noise", bot.OriginInternal, "", provider.RoleUser, valid + "noise"},
		{"missing newline", bot.OriginInternal, "", provider.RoleUser, strings.Replace(valid, ">\n", ">", 1)},
		{"array", bot.OriginInternal, "", provider.RoleUser, "<peer_message_outgoing>\n[]\n</peer_message_outgoing>"},
		{"null", bot.OriginInternal, "", provider.RoleUser, "<peer_message_outgoing>\nnull\n</peer_message_outgoing>"},
		{"two objects", bot.OriginInternal, "", provider.RoleUser, strings.Replace(valid, "\n</", "{}\n</", 1)},
		{"malformed", bot.OriginInternal, "", provider.RoleUser, "<peer_message_outgoing>\n{\n</peer_message_outgoing>"},
		{"different bot", bot.OriginInternal, "", provider.RoleUser, strings.Replace(valid, "bob@b", "other@b", 1)},
		{"nonexact bot", bot.OriginInternal, "", provider.RoleUser, strings.Replace(valid, "bob@b", " bob@b ", 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry := bot.Entry{Origin: tc.origin, EventID: tc.eventID, Message: provider.Message{Role: tc.role, Content: provider.TextContent(tc.text)}}
			if rows := remoteRows("ada", "bob@b", 7, entry, nil); len(rows) != 0 {
				t.Fatal("non-fact or untrusted origin produced an outgoing row")
			}
		})
	}
}

func TestPeerMessageRuntimeFactAlongsideLegacyHistory(t *testing.T) {
	root := t.TempDir()
	a := mustPeerBot(t, root, "ada", "u1")
	b := mustPeerBot(t, root, "bob", "u1")
	at := time.Now()
	appendRemoteCall(t, a, "native-sent", "bob@b", "old sent", "sent to bob@b", at)
	appendRemoteCall(t, a, "native-queued", "bob@b", "old queued", "gateway unreachable; queued", at)
	appendRemoteCall(t, a, "native-failed", "bob@b", "old failed", "remote: rejected", at)
	appendRemoteCall(t, a, "native-pending", "bob@b", "old pending", "", at)
	appendDirect(t, a, "remote_incoming", "bob@b", "incoming", at)
	appendBounce(t, a, "remote_bounce_env", "message to bob@b was not delivered: no such bot", at)
	appendBounce(t, a, "remote_undelivered_env", "message to bob@b was not delivered after 24h: old queued", at)
	text, err := RemoteOutgoingText(RemoteOutgoingFact{Bot: "bob@b", Text: "new unknown", Gateway: "unknown", CallID: "shell"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Append(bot.Entry{Time: at, Origin: bot.OriginInternal, Message: provider.Message{Role: provider.RoleUser, Content: provider.TextContent(text)}}); err != nil {
		t.Fatal(err)
	}
	rows, err := RemoteConversation(a, "bob@b")
	if err != nil || len(rows) != 8 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	byID := map[string]Direct{}
	for _, row := range rows {
		byID[row.ID] = row
	}
	for id, want := range map[string]string{"native-sent": statusSent, "native-queued": statusQueued, "native-failed": statusFailed, "native-pending": statusSent} {
		if byID[id].Status != want {
			t.Fatalf("legacy %s status=%s", id, byID[id].Status)
		}
	}
	if byID["remote_incoming"].From != "bob@b" || byID["remote_incoming"].Status != "" || byID["remote_bounce_env"].Kind != KindBounce || byID["remote_undelivered_env"].Kind != KindBounce {
		t.Fatal("incoming or bounce history changed")
	}
	local, err := Conversation(a, b)
	if err != nil || len(local) != 0 {
		t.Fatal("remote facts leaked into local conversation")
	}
}
