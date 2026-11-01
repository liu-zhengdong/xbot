//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/bot"
	"xbot/internal/remote"
)

func TestCLIPeerMessageRuntimeRecordFailureCurrentRoute(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("real source transcript permission failure needs an unprivileged process")
	}
	f := newCLIPeerRemoteFixture(t)
	original := f.Source
	const current = "PeerRecordCurrent"
	const body = "CURRENT_SOURCE_FROZEN_FULL_BODY\n完整正文在职责与私有记忆之外。"
	input := filepath.Join(f.A.Root, "work", original, "record-body.txt")
	if err := os.WriteFile(input, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	const cue = "CLI_PEER_MOVE_AND_SEND"
	command := `printf '%s\n' "$XBOT_CALL_ID" > record-original-call.txt` + "\n" + `"$XBOT_BIN" profile set --self --name ` + current + ` --json && XBOT_BOT=` + original + ` "$XBOT_BIN" peer-message ` + f.Address + ` --self --file record-body.txt --json`
	f.Model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Move original source then send and record through its current resource route", "command": command}))
	cliSkillSend(t, f.A, original, cue)
	results := f.Model.results(cue)
	if len(results) != 1 || !strings.HasPrefix(results[0], "exit 0\n") || len(commandOutput(results[0])) > 32<<10 {
		t.Fatal("same original Shell did not return bounded successful move and remote ACKs")
	}
	objects := strings.Split(strings.TrimSpace(commandOutput(results[0])), "\n")
	if len(objects) != 2 {
		t.Fatal("same source move/send lost two full JSON acknowledgements")
	}
	var moved struct {
		Name           string
		PreviousName   string `json:"previous_name"`
		Applied, Moved bool
		Error          string
	}
	var saved cliPeerMessageReply
	if json.Unmarshal([]byte(objects[0]), &moved) != nil || moved.Name != current || moved.PreviousName != original || !moved.Applied || !moved.Moved || moved.Error != "" || json.Unmarshal([]byte(objects[1]), &saved) != nil {
		t.Fatal("actual move or current source remote receipt did not complete")
	}
	f.Source = current
	callRaw, err := os.ReadFile(filepath.Join(f.A.Root, "work", current, "record-original-call.txt"))
	if err != nil || len(bytes.TrimSpace(callRaw)) == 0 {
		t.Fatal("same original Shell lost actual prepared call identity after source move")
	}
	if saved.Route != "remote" || saved.Posted || saved.Gateway != "accepted" || saved.EnvelopeID == "" || saved.Queued || saved.Error != "" {
		t.Fatal("current source outgoing receipt lost actual gateway accepted facts")
	}
	f.fact(t, saved, strings.TrimSpace(string(callRaw)), body)
	f.consumer(t, []cliPeerMessageReply{saved}, []string{body}, []string{"sent"})
	// Existing actual tail is reopened for each AppendSeq; deny that precise
	// file, not only the parent directory or the CLI child's resource limit.
	tails, err := filepath.Glob(filepath.Join(f.A.Root, "bots", current, "transcript", "seg-*-open.jsonl"))
	if err != nil || len(tails) != 1 {
		t.Fatal("actual source has no unique initialized transcript tail for physical record fault")
	}
	tail := tails[0]
	info, err := os.Stat(tail)
	if err != nil {
		t.Fatal(err)
	}
	mode := info.Mode().Perm()
	t.Cleanup(func() { _ = os.Chmod(tail, mode) })
	before := filepath.Join(f.A.Root, "work", current, "record-tail-before.jsonl")
	after := filepath.Join(f.A.Root, "work", current, "record-tail-after.jsonl")
	const failedBody = "ACTUAL_GATEWAY_ACCEPTED_BUT_SOURCE_RECORD_DENIED"
	failure := `cp ` + shellQuote(tail) + ` ` + shellQuote(before) + ` && chmod 0400 ` + shellQuote(tail) + "\n" + `"$XBOT_BIN" peer-message ` + f.Address + ` --self --text ` + failedBody + ` --json` + "\n" + `peer_status=$?` + "\n" + `chmod ` + fmt.Sprintf("%04o", mode) + ` ` + shellQuote(tail) + "\n" + `cp ` + shellQuote(tail) + ` ` + shellQuote(after) + "\n" + `exit "$peer_status"`
	const failureCue = "CLI_PEER_PHYSICAL_RECORD_DENIAL"
	f.Model.add(failureCue, mcpRuntimeTool("sh", map[string]string{"description": "Observe actual source transcript write failure after genuine gateway acceptance", "command": failure}))
	cliSkillSend(t, f.A, current, failureCue)
	failed := f.Model.results(failureCue)
	if len(failed) != 1 || len(commandOutput(failed[0])) > 32<<10 {
		t.Fatal("actual record failure output lost bounded one-result facts")
	}
	exit, _, ok := strings.Cut(failed[0], "\n")
	var reply cliPeerMessageReply
	if !ok || !exitLine.MatchString(exit) || exit == "exit 0" || json.NewDecoder(strings.NewReader(commandOutput(failed[0]))).Decode(&reply) != nil || reply.Route != "remote" || reply.Posted || reply.Gateway != "accepted" || reply.GatewayStatus != 0 || reply.EnvelopeID == "" || reply.Queued || reply.Recorded == nil || *reply.Recorded || reply.RecordSeq != 0 || reply.Stage != "record" || reply.Error == "" {
		t.Fatal("actual record error discarded gateway accepted facts or falsely acknowledged a source record")
	}
	beforeRaw, err := os.ReadFile(before)
	if err != nil {
		t.Fatal(err)
	}
	afterRaw, err := os.ReadFile(after)
	if err != nil || !bytes.Equal(beforeRaw, afterRaw) {
		t.Fatal("physical failed record appended a partial source line")
	}
	if info, err := os.Stat(tail); err != nil || info.Mode().Perm() != mode {
		t.Fatal("actual source tail mode was not restored before ordinary tool result append")
	}
	accepted := f.accepted(t)
	if len(accepted) != 2 || accepted[0].ID != saved.EnvelopeID || accepted[1].ID != reply.EnvelopeID {
		t.Fatal("record failure was not after a second real gateway acceptance, or blindly retried the envelope")
	}
	f.consumer(t, []cliPeerMessageReply{saved}, []string{body}, []string{"sent"})
	source, err := bot.Load(f.A.Root, current)
	if err != nil {
		t.Fatal(err)
	}
	facts := 0
	if err := source.Walk(1, 0, func(seq int, e bot.Entry) error {
		if e.Origin == bot.OriginInternal && e.Message.Role == "user" && e.EventID == "" && strings.HasPrefix(e.Message.Text(), "<peer_message_outgoing>\n") {
			facts++
			if seq != saved.RecordSeq || e.Branch != "" {
				t.Fatal("foreground original execution fact moved to an invented/latest branch")
			}
		}
		return nil
	}); err != nil || facts != 1 {
		t.Fatal("failed physical record invented another outgoing fact")
	}
	f.B.Restart()
	cliPeerRemoteWait(t, "both genuinely accepted envelopes at the actual receiver", func() bool { return len(f.events(t)) == 2 })
	ids := map[string]bool{}
	for _, ev := range f.events(t) {
		if ev.ID != "remote_"+f.SenderID+"_"+saved.EnvelopeID && ev.ID != "remote_"+f.SenderID+"_"+reply.EnvelopeID {
			t.Fatal("receiver event did not match the two real accepted envelope IDs")
		}
		if ids[ev.ID] || ev.SenderKind != "bot" || !ev.Untrusted || ev.Hops != 1 || ev.Chain != "ch_"+current {
			t.Fatal("record error caused duplicate or changed original current-source Bot chain")
		}
		ids[ev.ID] = true
	}
	// Genuine peer unpair notice retains A's paired-device history entry.
	if err := remote.RemoveDevice(f.B.Root, "sender"); err != nil {
		t.Fatal("actual paired receiver could not send original unpair notice")
	}
	cliPeerRemoteWait(t, "source paired device unpaired mark", func() bool {
		cfg, err := remote.Load(f.A.Root)
		if err != nil {
			return false
		}
		d, found := cfg.ByName("receiver")
		return found && !d.UnpairedAt.IsZero()
	})
	f.consumer(t, []cliPeerMessageReply{saved}, []string{body}, []string{"sent"})
	for _, path := range []string{filepath.Join(f.A.Root, "bots", original), filepath.Join(f.A.Root, "work", original)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("old source directory was recreated by current recording or history consumer")
		}
	}
}

func TestCLIPeerMessageRuntimeUnknownQueueFailure(t *testing.T) {
	f := newCLIPeerRemoteFixture(t)
	outbox := filepath.Join(f.A.Root, "remote", "outbox")
	if _, err := os.Stat(outbox); !os.IsNotExist(err) {
		t.Fatal("fresh actual sender has unexpected outbox state before physical queue fault")
	}
	// A file at the required directory is a real enqueue filesystem failure;
	// original Run is left untouched and no existing queue/rate is cleared.
	if err := os.WriteFile(outbox, []byte("PHYSICAL_QUEUE_DIRECTORY_CONFLICT\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.loseBody = true
	f.mu.Unlock()
	const cue = "CLI_PEER_UNKNOWN_NO_QUEUE"
	const body = "GATEWAY_STORED_RESPONSE_LOST_QUEUE_FAILED"
	f.Model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Observe genuine accepted-but-response-lost send with real enqueue filesystem failure", "command": `printf '%s\n' "$XBOT_CALL_ID" > unknown-original-call.txt` + "\n" + `"$XBOT_BIN" peer-message ` + f.Address + ` --self --text ` + body + ` --json`}))
	cliSkillSend(t, f.A, f.Source, cue)
	results := f.Model.results(cue)
	if len(results) != 1 || len(commandOutput(results[0])) > 32<<10 {
		t.Fatal("unknown queue error lacks bounded actual source ACK")
	}
	exit, _, ok := strings.Cut(results[0], "\n")
	var reply cliPeerMessageReply
	if !ok || !exitLine.MatchString(exit) || exit == "exit 0" || json.NewDecoder(strings.NewReader(commandOutput(results[0]))).Decode(&reply) != nil || reply.Route != "remote" || reply.Posted || reply.Gateway != "unknown" || reply.GatewayStatus != 0 || reply.EnvelopeID == "" || reply.Queued || reply.Stage != "queue" || reply.Recorded == nil || !*reply.Recorded || reply.RecordSeq <= 0 || reply.Error == "" {
		t.Fatal("unknown accepted response loss plus enqueue failure was misreported as unsent or queued")
	}
	accepted := f.accepted(t)
	if len(accepted) != 1 || accepted[0].ID != reply.EnvelopeID {
		t.Fatal("unknown did not follow one genuine accepted original envelope")
	}
	call, err := os.ReadFile(filepath.Join(f.A.Root, "work", f.Source, "unknown-original-call.txt"))
	if err != nil || len(bytes.TrimSpace(call)) == 0 {
		t.Fatal("unknown send lost its original native Shell call identity")
	}
	f.fact(t, reply, strings.TrimSpace(string(call)), body)
	f.consumer(t, []cliPeerMessageReply{reply}, []string{body}, []string{"unknown"})
	raw, err := os.ReadFile(outbox)
	if err != nil || string(raw) != "PHYSICAL_QUEUE_DIRECTORY_CONFLICT\n" {
		t.Fatal("failed enqueue silently replaced physical fault state")
	}
	f.mu.Lock()
	f.loseBody = false
	f.mu.Unlock()
	f.B.Restart()
	cliPeerRemoteWait(t, "accepted unknown envelope at original receiver", func() bool { return len(f.events(t)) == 1 })
	ev := f.events(t)[0]
	if ev.ID != "remote_"+f.SenderID+"_"+reply.EnvelopeID || ev.SenderKind != "bot" || !ev.Untrusted || ev.Hops != 1 || ev.Chain != "ch_"+f.Source || ev.Content != body {
		t.Fatal("false queued state was confused with actual accepted Bot delivery")
	}
	if len(f.accepted(t)) != 1 {
		t.Fatal("unknown queue failure caused an automatic fresh envelope retry")
	}
}
