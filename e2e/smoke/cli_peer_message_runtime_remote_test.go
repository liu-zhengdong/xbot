//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"xbot/internal/gateway"
)

func TestCLIPeerMessageRuntimeRemoteAcceptedBodyConsumer(t *testing.T) {
	f := newCLIPeerRemoteFixture(t)
	const head, tail = "REMOTE_FILE_PUBLIC_BEGIN", "REMOTE_PUBLIC_END"
	// U+0001 is valid UTF-8, not TrimSpace whitespace, and JSON escapes it to
	// six bytes. The full valid8KiB request cannot be hidden in a32KiB ACK.
	body := head + strings.Repeat("\x01", (8<<10)-len(head)-len(tail)) + tail
	encoded, err := json.Marshal(body)
	if err != nil || len(body) != 8<<10 || !utf8.ValidString(body) || len(encoded) <= 32<<10 {
		t.Fatal("accepted body fixture must be complete8KiB UTF-8 with JSON larger than32KiB")
	}
	workspace := filepath.Join(f.A.Root, "work", f.Source)
	filePath := filepath.Join(workspace, "accepted-file.txt")
	if err := os.WriteFile(filePath, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	first, firstCall := f.run(t, "PM_REMOTE_ACCEPT_FILE", `"$XBOT_BIN" peer-message `+shellQuote(f.Address)+` --self --file accepted-file.txt --json`)
	assertAccepted := func(reply cliPeerMessageReply) {
		t.Helper()
		if reply.Bot != f.Address || reply.Route != "remote" || reply.Posted || reply.EventID != "" || reply.EnvelopeID == "" || reply.Gateway != "accepted" || reply.GatewayStatus != 0 || reply.Queued || reply.Stage != "complete" || reply.Error != "" || reply.Recorded == nil || !*reply.Recorded || reply.RecordSeq <= 0 {
			t.Fatal("accepted remote ACK claimed delivery/HTTP status or lost actual envelope/record facts")
		}
	}
	assertAccepted(first)
	// Replace the original input before reading the real consumer. The ordinary
	// fact must retain the frozen request, not reload a file or parse Shell args.
	if _, err := os.ReadFile(filePath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte("REPLACED_INPUT_MUST_NOT_BECOME_HISTORY"), 0600); err != nil {
		t.Fatal(err)
	}
	f.fact(t, first, firstCall, body)
	f.consumer(t, []cliPeerMessageReply{first}, []string{body}, []string{"sent"})
	stdinBody := strings.Replace(body, "FILE", "PIPE", 1)
	stdinPath := filepath.Join(workspace, "accepted-stdin.txt")
	if err := os.WriteFile(stdinPath, []byte(stdinBody), 0600); err != nil {
		t.Fatal(err)
	}
	second, secondCall := f.run(t, "PM_REMOTE_ACCEPT_STDIN", `cat accepted-stdin.txt | "$XBOT_BIN" peer-message `+shellQuote(f.Address)+` --self --file - --json`)
	assertAccepted(second)
	if first.EnvelopeID == second.EnvelopeID || first.RecordSeq == second.RecordSeq || firstCall == secondCall {
		t.Fatal("independent actual file/stdin sends reused an envelope, source seq or Shell call identity")
	}
	// This resolved path is the exact fixture just written above.
	if _, err := os.ReadFile(stdinPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(stdinPath); err != nil {
		t.Fatal(err)
	}
	f.fact(t, second, secondCall, stdinBody)
	replies := []cliPeerMessageReply{first, second}
	bodies := []string{body, stdinBody}
	f.consumer(t, replies, bodies, []string{"sent", "sent"})
	accepted := f.accepted(t)
	if len(accepted) != 2 || accepted[0].ID != first.EnvelopeID || accepted[1].ID != second.EnvelopeID {
		t.Fatal("actual signed TCP gateway acceptance does not match both native ACK envelope identities")
	}
	// B has remained stopped throughout both sends; A had no cached directory
	// of TargetB. Only now does the real receiver subprocess poll and decrypt.
	receiverStart := time.Now()
	f.B.Restart()
	cliPeerRemoteWait(t, "two receiver inbox acceptances", func() bool { return len(f.events(t)) >= 2 })
	events := f.events(t)
	if len(events) != 2 {
		t.Fatal("actual receiver duplicated or lost an accepted remote message")
	}
	byID := make(map[string]cliPeerRemoteEvent)
	for _, event := range events {
		byID[event.ID] = event
	}
	for i, reply := range replies {
		env := accepted[i]
		event, found := byID["remote_"+f.SenderID+"_"+reply.EnvelopeID]
		// This fixture has no secret or untrusted fence to rewrite. Source fact
		// comparison above is to the original request, independently of receive.
		if !found || env.Kind != "box" || env.From != f.SenderID || env.To != f.TargetID || event.Source != "bot:"+f.Source+"@sender" || event.Type != "message" || event.Sender != f.Source+"@sender" || event.SenderKind != "bot" || !event.Untrusted || event.Hops != 1 || event.Chain != "ch_"+f.Source || event.Content != bodies[i] || event.Time.Before(receiverStart) || !event.Time.After(env.Time) {
			t.Fatal("actual receiver event lost signed origin, stable ID, public body, chain/hops or actual receive-time provenance")
		}
	}
	// Receipt/history remains the original accepted fact; receiving does not
	// rewrite source history or turn a gateway receipt into an 'already read'.
	f.consumer(t, replies, bodies, []string{"sent", "sent"})
}

func TestCLIPeerMessageRuntimeRemoteQueuedSameEnvelope(t *testing.T) {
	f := newCLIPeerRemoteFixture(t)
	f.mu.Lock()
	f.loseBody = true
	f.mu.Unlock()
	const body = "REMOTE_QUEUED_PUBLIC_BODY\nKeep the original frozen payload and same signed envelope."
	inputPath := filepath.Join(f.A.Root, "work", f.Source, "queued-input.txt")
	if err := os.WriteFile(inputPath, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	reply, callID := f.run(t, "PM_REMOTE_QUEUE_ONCE", `"$XBOT_BIN" peer-message `+shellQuote(f.Address)+` --self --file queued-input.txt --json`)
	if reply.Bot != f.Address || reply.Route != "remote" || reply.Posted || reply.EventID != "" || reply.EnvelopeID == "" || reply.Gateway != "unknown" || reply.GatewayStatus != 0 || !reply.Queued || reply.Stage != "complete" || reply.Error != "" || reply.Recorded == nil || !*reply.Recorded || reply.RecordSeq <= 0 {
		t.Fatal("accepted response-body loss did not preserve unknown gateway plus durable queued/recorded facts")
	}
	accepted := f.accepted(t)
	if len(accepted) == 0 || accepted[0].ID != reply.EnvelopeID {
		t.Fatal("body-read failure did not follow a real authenticated gateway acceptance")
	}
	original := accepted[0]
	outboxPath := filepath.Join(f.A.Root, "remote", "outbox", reply.EnvelopeID+".json")
	queuedRaw, err := os.ReadFile(outboxPath)
	if err != nil {
		t.Fatal("queued ACK has no actual original durable outbox item")
	}
	var queued struct {
		Env      gateway.Envelope
		FromBot  string `json:"from_bot"`
		To       string
		Created  time.Time
		Attempts int
		Next     time.Time
	}
	if err := json.Unmarshal(queuedRaw, &queued); err != nil || !reflect.DeepEqual(queued.Env, original) || queued.FromBot != f.Source || queued.To != f.Address || queued.Created.IsZero() || queued.Attempts < 1 || !queued.Next.After(queued.Created) {
		t.Fatal("actual outbox changed the original signed envelope or source/retry metadata")
	}
	if queued.Attempts == 1 && !queued.Next.Equal(queued.Created.Add(5*time.Second)) {
		t.Fatal("newly queued item did not retain its original genuine Next+5s")
	}
	info, err := os.Stat(outboxPath)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("actual persistent retry item lost0600")
	}
	// Original file changes cannot alter the ordinary fact or its Web consumer.
	if _, err := os.ReadFile(inputPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inputPath, []byte("QUEUED_SOURCE_FILE_WAS_REPLACED"), 0600); err != nil {
		t.Fatal(err)
	}
	f.fact(t, reply, callID, body)
	f.consumer(t, []cliPeerMessageReply{reply}, []string{body}, []string{"queued"})
	receiverStart := time.Now()
	f.B.Restart()
	cliPeerRemoteWait(t, "original queued envelope receiver acceptance", func() bool { return len(f.events(t)) >= 1 })
	f.mu.Lock()
	f.loseBody = false
	f.mu.Unlock()
	// Let the actual five-second worker retry; no flush helper, synthetic clock,
	// rate reset, outbox edit or second CLI send manufactures the retry.
	cliPeerRemoteWait(t, "real outbox retry settlement", func() bool {
		_, err := os.Stat(outboxPath)
		return os.IsNotExist(err)
	})
	accepted = f.accepted(t)
	if len(accepted) < 2 {
		t.Fatal("original durable outbox never retried its accepted-but-response-lost envelope")
	}
	for _, env := range accepted {
		if !reflect.DeepEqual(env, original) || !bytes.Equal(env.Body, original.Body) || !bytes.Equal(env.Sig, original.Sig) {
			t.Fatal("actual retry rebuilt the envelope ID, ciphertext, timestamp or signature")
		}
	}
	events := f.events(t)
	if len(events) != 1 {
		t.Fatal("gateway duplicate handling/remote seen identity duplicated actual recipient acceptance")
	}
	event := events[0]
	if event.ID != "remote_"+f.SenderID+"_"+reply.EnvelopeID || event.Source != "bot:"+f.Source+"@sender" || event.Type != "message" || event.Sender != f.Source+"@sender" || event.SenderKind != "bot" || !event.Untrusted || event.Hops != 1 || event.Chain != "ch_"+f.Source || event.Content != body || event.Time.Before(receiverStart) || !event.Time.After(original.Time) {
		t.Fatal("queued delivery lost its actual stable provenance or receiver-time semantics")
	}
	f.fact(t, reply, callID, body)
	f.consumer(t, []cliPeerMessageReply{reply}, []string{body}, []string{"queued"})
}
