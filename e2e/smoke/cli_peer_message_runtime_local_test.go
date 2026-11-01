//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCLIPeerMessageRuntimeLocalSource(t *testing.T) {
	cliSkillIsolation(t)
	model := newCLIPeerMessageModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const source, target, shared, foreign, orphan = "PeerLocalSource", "PeerLocalTarget", "PeerLocalShared", "PeerLocalForeign", "PeerLocalOrphan"
	for _, name := range []string{source, target, shared, foreign, orphan} {
		srv.CreateBot(name, "PEER_SOURCE_PRIVATE_NEVER_SENT: keep private persona and memory local.")
	}
	var primary struct{ Name string }
	primaryRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil || json.Unmarshal(primaryRaw, &primary) != nil || primary.Name == "" || primary.Name == source {
		t.Fatal("actual peer-message source must be nonprimary")
	}
	var realOwners []string
	const extraOwner = "peer-local-shared-owner"
	for _, name := range []string{source, shared, foreign, orphan} {
		path := filepath.Join(srv.Root, "bots", name, "config.json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var cfg map[string]json.RawMessage
		if json.Unmarshal(raw, &cfg) != nil {
			t.Fatal("invalid actual API config")
		}
		var owners []string
		if json.Unmarshal(cfg["owners"], &owners) != nil || len(owners) == 0 {
			t.Fatal("actual source owner absent")
		}
		if name == source {
			realOwners = slices.Clone(owners)
			if slices.Contains(owners, extraOwner) {
				t.Fatal("extra owner fixture collision")
			}
			owners = append(owners, extraOwner)
		}
		if name == shared {
			owners = []string{extraOwner}
		}
		if name == foreign {
			owners = []string{"peer-local-other-owner"}
		}
		if name == orphan {
			owners = []string{}
		}
		cfg["owners"], err = json.Marshal(owners)
		if err != nil {
			t.Fatal(err)
		}
		raw, err = json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if len(realOwners) == 0 {
		t.Fatal("source fixture lost its authenticated owner")
	}
	// Persist every ownership change before actual Resources capture/first CLI.
	srv.Restart()
	run := func(cue, command string) string {
		t.Helper()
		model.base.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Send original prepared source local peer message through actual Bus Inbox", "command": command}))
		cliSkillSend(t, srv, source, cue)
		results := model.base.results(cue)
		if len(results) != 1 {
			t.Fatalf("local cue=%s count=%d", cue, len(results))
		}
		if results[0] == "" || len(commandOutput(results[0])) > 32<<10 {
			t.Fatalf("local cue=%s empty=%t bytes=%d", cue, results[0] == "", len(commandOutput(results[0])))
		}
		return results[0]
	}
	// Observe complete persisted post rows, including those the real consumer
	// already acknowledged, rather than treating Pending as delivery proof.
	type event struct {
		ID, Source, Type, Content, Sender, Chain string
		SenderKind                               string `json:"sender_kind"`
		Untrusted                                bool
		Hops                                     int
		Time                                     time.Time
	}
	events := func(name string) []event {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(srv.Root, "bots", name, "inbox.jsonl"))
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			t.Fatal(err)
		}
		var got []event
		for _, line := range bytes.Split(raw, []byte{'\n'}) {
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var rec struct {
				Op    string
				Event *event
			}
			if json.Unmarshal(line, &rec) != nil {
				t.Fatal("incomplete actual Inbox JSON record")
			}
			if rec.Op == "post" && rec.Event != nil && rec.Event.Source == "bot:"+source {
				got = append(got, *rec.Event)
			}
		}
		return got
	}
	body := "PEER_FULL_BEGIN\n" + strings.Repeat("完整原消息正文保持。", 260) + "\nPEER_FULL_END"
	if len(body) > 8192 {
		t.Fatal("base UTF8 fixture too large")
	}
	body += strings.Repeat("x", 8192-len(body))
	workspace := filepath.Join(srv.Root, "work", source)
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workspace, "peer-complete.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat(" ", 20<<10)+body+"\n\t"), 0600); err != nil {
		t.Fatal(err)
	}
	var receipts []cliPeerMessageReply
	for i, command := range []string{`XBOT_BOT=` + primary.Name + ` "$XBOT_BIN" peer-message ` + target + ` --self --file peer-complete.txt --json`, `cat peer-complete.txt | "$XBOT_BIN" peer-message ` + target + ` --self --file - --json`} {
		var reply cliPeerMessageReply
		cliBotDecode(t, run(fmt.Sprintf("CLI_PEER_LOCAL_FULL_%d", i), command), &reply)
		if reply.Bot != target || reply.Route != "local" || !reply.Posted || reply.EventID == "" || reply.EnvelopeID != "" || reply.Gateway != "" || reply.GatewayStatus != 0 || reply.Queued || reply.Stage != "complete" || reply.Recorded != nil || reply.RecordSeq != 0 || reply.Error != "" {
			t.Fatal("actual local receipt lost original accepted event or invented remote/record facts")
		}
		receipts = append(receipts, reply)
	}
	got := events(target)
	if len(got) != 2 {
		t.Fatalf("actual accepted full local events=%d want2", len(got))
	}
	for i, ev := range got {
		if ev.ID != receipts[i].EventID || ev.ID == "" || ev.Type != "message" || ev.Sender != source || ev.SenderKind != "bot" || !ev.Untrusted || ev.Hops != 1 || ev.Chain != "ch_"+source || ev.Time.IsZero() || ev.Content != body || strings.Contains(ev.Content, "PEER_SOURCE_PRIVATE_NEVER_SENT") {
			t.Fatal("full local original Event provenance/body differs from actual source receipt")
		}
	}
	const observe = "CLI_PEER_RECIPIENT_OBSERVATION"
	model.base.add(observe)
	cliSkillSend(t, srv, target, observe)
	// absorbInbox appends real Bot user messages after the owner input. The
	// latest user may therefore be the Inbox event, not the observation cue.
	// Inspect only actual target system requests instead of that cue selector.
	fullSeen, targetRequests := false, 0
	model.mu.Lock()
	for _, raw := range model.raw {
		var req stubRequest
		if json.Unmarshal(raw, &req) != nil {
			continue
		}
		isTarget := false
		for _, msg := range req.Messages {
			if msg.Role == "system" && strings.Contains(msg.Content, "BOTNAME="+target) {
				isTarget = true
			}
		}
		if !isTarget {
			continue
		}
		targetRequests++
		for _, msg := range req.Messages {
			if msg.Role == "user" && strings.Contains(msg.Content, body) && strings.Contains(msg.Content, "bot:"+source) {
				fullSeen = true
			}
		}
	}
	model.mu.Unlock()
	if !fullSeen {
		t.Fatalf("actual target model context did not consume complete untrusted source event; target_requests=%d", targetRequests)
	}
	var visible cliPeerMessageReply
	cliBotDecode(t, run("CLI_PEER_SHARED_ACCEPT", `"$XBOT_BIN" peer-message `+shared+` --self --text SHARED_OWNER_ACCEPTED --json`), &visible)
	if !visible.Posted || visible.Route != "local" || visible.EventID == "" || visible.Error != "" || visible.Recorded != nil {
		t.Fatal("source local shared owner intersection was replaced by Web owner access")
	}
	sharedEvents := events(shared)
	if len(sharedEvents) != 1 || sharedEvents[0].ID != visible.EventID || sharedEvents[0].Content != "SHARED_OWNER_ACCEPTED" || !sharedEvents[0].Untrusted || sharedEvents[0].SenderKind != "bot" {
		t.Fatal("shared-local acceptance lacks original Inbox event")
	}
	ratePath := filepath.Join(srv.Root, "peer-rate.json")
	for i, test := range []struct{ target, text, prefix string }{{source, "SELF_REJECT", ""}, {foreign, "FOREIGN_REJECT", ""}, {orphan, "ORPHAN_REJECT", ""}, {target, " ", ""}, {target, strings.Repeat("z", 8193), ""}, {target, "NO_CALL_REJECT", "env -u XBOT_CALL_ID "}} {
		before, err := os.ReadFile(ratePath)
		if err != nil {
			t.Fatal(err)
		}
		beforeTarget := len(events(target))
		beforeShared := len(events(shared))
		input := filepath.Join(workspace, fmt.Sprintf("peer-invalid-%d.txt", i))
		if err := os.WriteFile(input, []byte(test.text), 0600); err != nil {
			t.Fatal(err)
		}
		result := run(fmt.Sprintf("CLI_PEER_REFUSAL_%d", i), test.prefix+`"$XBOT_BIN" peer-message `+test.target+` --self --file `+filepath.Base(input)+` --json`)
		exit, _, ok := strings.Cut(result, "\n")
		var reply cliPeerMessageReply
		if !ok || !exitLine.MatchString(exit) || exit == "exit 0" || json.NewDecoder(strings.NewReader(commandOutput(result))).Decode(&reply) != nil || reply.Route != "local" || reply.Posted || reply.EventID != "" || reply.EnvelopeID != "" || reply.Queued || reply.Recorded != nil || reply.RecordSeq != 0 || reply.Error == "" {
			t.Fatal("local false source/target/text refusal did not preserve typed pre-post facts")
		}
		after, err := os.ReadFile(ratePath)
		if err != nil || !bytes.Equal(before, after) || len(events(target)) != beforeTarget || len(events(shared)) != beforeShared || len(events(foreign)) != 0 || len(events(orphan)) != 0 || len(events(source)) != 0 {
			t.Fatal("validation refusal changed peer rate/accepted local events")
		}
	}
}
