//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// Observe complete persisted records, including posts already acknowledged by
// the actual consumer. Pending state is not a delivery or wake-up guarantee.
type cliChatGroupPostEvent struct {
	ID, Source, Type, Content, Sender string
	SenderKind                        string `json:"sender_kind"`
	Time                              time.Time
	Untrusted                         bool
	Hops                              int
	Chain                             string
}

func cliChatGroupPostRecords(t *testing.T, path string) []json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var records []json.RawMessage
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if !json.Valid(line) {
			t.Fatal("actual persisted group/inbox record is not complete JSON")
		}
		records = append(records, append(json.RawMessage(nil), line...))
	}
	return records
}

func cliChatGroupPostEvents(t *testing.T, root, name string) []cliChatGroupPostEvent {
	t.Helper()
	var events []cliChatGroupPostEvent
	for _, raw := range cliChatGroupPostRecords(t, filepath.Join(root, "bots", name, "inbox.jsonl")) {
		var record struct {
			Op    string
			Event *cliChatGroupPostEvent
		}
		if err := json.Unmarshal(raw, &record); err != nil {
			t.Fatal(err)
		}
		if record.Op == "post" && record.Event != nil {
			events = append(events, *record.Event)
		}
	}
	return events
}

func cliChatGroupPostRun(t *testing.T, srv *Server, model *mcpRuntimeModel, source, cue, command string) string {
	t.Helper()
	model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Exercise actual source chat-group creation and prepared Shell post", "command": command}))
	cliSkillSend(t, srv, source, cue)
	results := model.results(cue)
	if len(results) != 1 || results[0] == "" || len(commandOutput(results[0])) > 32<<10 {
		t.Fatal("actual source chat-group operation did not return one bounded Shell result")
	}
	return results[0]
}

func cliChatGroupPostDelivery(t *testing.T, root, source, target, group string, lines []cliChatGroupLine) {
	t.Helper()
	var got []cliChatGroupPostEvent
	for _, event := range cliChatGroupPostEvents(t, root, target) {
		if event.Source == "group:"+group {
			got = append(got, event)
		}
	}
	if len(got) != len(lines) {
		t.Fatal("actual target inbox lost or duplicated accepted group post records")
	}
	seen := make(map[string]bool)
	for i, event := range got {
		line := lines[i]
		body := fmt.Sprintf("<group id=%q from=%q kind=%q>\n%s\n</group>", group, source, "bot", line.Text)
		if event.ID == "" || seen[event.ID] || event.Type != "message" || event.Sender != source || event.SenderKind != "bot" || !event.Untrusted || event.Hops != 1 || event.Chain != "ch_"+source || !event.Time.Equal(line.Time) || event.Content != body {
			t.Fatal("actual inbox event lost source bot/untrusted/chain/hops/time or copied content beyond the exact public group body")
		}
		seen[event.ID] = true
	}
}

func TestCLIChatGroupRuntimeCreatePostActualSource(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const source, b, c, foreign = "ChatPostA", "ChatPostB", "ChatPostC", "ChatPostForeign"
	for _, name := range []string{source, b, c, foreign} {
		srv.CreateBot(name, "Use only the explicit public group message, keeping private duties and memory local.")
	}
	primaryRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var primary struct{ Name string }
	if err := json.Unmarshal(primaryRaw, &primary); err != nil || primary.Name == "" || primary.Name == source {
		t.Fatal("chat-group creation/post fixture must use an actual nonprimary source")
	}
	var sourceOwners []string
	for _, name := range []string{source, b, c, foreign} {
		path := filepath.Join(srv.Root, "bots", name, "config.json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var cfg map[string]json.RawMessage
		if err := json.Unmarshal(raw, &cfg); err != nil {
			t.Fatal(err)
		}
		var owners []string
		if err := json.Unmarshal(cfg["owners"], &owners); err != nil || len(owners) == 0 || owners[0] == "" {
			t.Fatal("actual API Bot lacks authenticated owners")
		}
		if name == source {
			sourceOwners = owners
		} else if !slices.Contains(owners, sourceOwners[0]) {
			t.Fatal("actual API peers lack the authenticated source owner intersection")
		}
		if name == foreign {
			const foreignOwner = "chat-post-foreign-fixture-owner"
			if slices.Contains(sourceOwners, foreignOwner) {
				t.Fatal("controlled foreign owner collides with actual source owners")
			}
			cfg["owners"], _ = json.Marshal([]string{foreignOwner})
			updated, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, updated, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	// All owner fixture edits finish before the source's first runtime call and
	// the private restart that recaptures persisted resources.
	srv.Restart()
	run := func(cue, command string) string {
		return cliChatGroupPostRun(t, srv, model, source, cue, command)
	}
	beforeConfig := make(map[string][]byte)
	for _, name := range []string{source, b, c, foreign} {
		raw, err := os.ReadFile(filepath.Join(srv.Root, "bots", name, "config.json"))
		if err != nil {
			t.Fatal(err)
		}
		beforeConfig[name] = raw
	}
	var created cliChatGroupCreateReply
	// Create has no original-turn requirement. Repeated self/peer member values
	// exercise the actual Bus auto-self, deduplication and sorted local members.
	cliBotDecode(t, run("CLI_CHAT_POST_CREATE", `env -u XBOT_CALL_ID "$XBOT_BIN" chat-group create ' Actual source room ' --self --member `+c+` --member `+b+` --member `+source+` --member `+b+` --json`), &created)
	if !created.Created || created.Group == nil || created.Error != "" {
		t.Fatal("actual nonprimary source did not create its member group without Call-ID")
	}
	group := *created.Group
	if !strings.HasPrefix(group.ID, "g_") || len(group.ID) != 10 || group.Name != "Actual source room" || !slices.Equal(group.Members, []string{source, b, c}) || group.Created.IsZero() {
		t.Fatal("actual group creation lost trimmed name, random identity, Created or sorted self/visible members")
	}
	groupDir := filepath.Join(srv.Root, "groups", group.ID)
	metaRaw, err := os.ReadFile(filepath.Join(groupDir, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta cliChatGroup
	var fields map[string]json.RawMessage
	if json.Unmarshal(metaRaw, &meta) != nil || json.Unmarshal(metaRaw, &fields) != nil || len(fields) != 4 || !reflect.DeepEqual(meta, group) {
		t.Fatal("actual group metadata does not match the complete creation fields")
	}
	for _, invalid := range []struct{ cue, args string }{
		{"CLI_CHAT_POST_CREATE_EMPTY", `'  ' --member ` + b},
		{"CLI_CHAT_POST_CREATE_SELF_ONLY", `'Self only' --member ` + source},
		{"CLI_CHAT_POST_CREATE_FOREIGN", `'Foreign member' --member ` + foreign},
	} {
		entries, err := os.ReadDir(filepath.Join(srv.Root, "groups"))
		if err != nil {
			t.Fatal(err)
		}
		result := run(invalid.cue, `"$XBOT_BIN" chat-group create `+invalid.args+` --self --json`)
		exit, _, ok := strings.Cut(result, "\n")
		var refusal cliChatGroupCreateReply
		if !ok || !exitLine.MatchString(exit) || exit == "exit 0" || json.NewDecoder(strings.NewReader(commandOutput(result))).Decode(&refusal) != nil || refusal.Created || refusal.Group != nil || refusal.Error == "" {
			t.Fatal("invalid empty/self-only/foreign create did not return an honest typed refusal")
		}
		after, err := os.ReadDir(filepath.Join(srv.Root, "groups"))
		if err != nil || len(after) != len(entries) {
			t.Fatal("rejected group creation persisted an extra group")
		}
	}
	const head, tail = "CHAT_PUBLIC_FULL_BEGIN\n", "\nCHAT_PUBLIC_FULL_END"
	remaining := (8 << 10) - len(head) - len(tail)
	body := head + strings.Repeat("界", remaining/3) + strings.Repeat("x", remaining%3) + tail
	if len(body) != 8<<10 || !utf8.ValidString(body) {
		t.Fatal("post fixture must contain exactly8192 complete UTF-8 bytes")
	}
	input := filepath.Join(srv.Root, "work", source, "public-group-body.txt")
	if err := os.WriteFile(input, []byte(" \t"+body+"\n "), 0600); err != nil {
		t.Fatal(err)
	}
	var postedIDs []string
	for _, post := range []struct{ cue, command string }{
		{"CLI_CHAT_POST_FILE", `"$XBOT_BIN" chat-group post ` + group.ID + ` --self --file public-group-body.txt --json`},
		{"CLI_CHAT_POST_STDIN", `cat public-group-body.txt | "$XBOT_BIN" chat-group post ` + group.ID + ` --self --file - --json`},
	} {
		var reply cliChatGroupPostReply
		cliBotDecode(t, run(post.cue, post.command), &reply)
		if reply.ID != group.ID || !reply.Appended || reply.MessageID == "" || !slices.Equal(reply.PostedMembers, []string{b, c}) || reply.FailedMember != "" || reply.Stage != "complete" || reply.Error != "" {
			t.Fatal("actual prepared Shell post lost its append/fanout commit receipt")
		}
		postedIDs = append(postedIDs, reply.MessageID)
		// cliSkillSend uses the real authenticated owner messages route. Its
		// original WebEvent has empty Chain/Hops0 and is passed directly to
		// Handle when idle; it need not appear in the persisted source inbox.
		// Recipient records below prove ch_SOURCE fallback and Hops1.
	}
	transcript := filepath.Join(groupDir, "transcript.jsonl")
	var lines []cliChatGroupLine
	for _, raw := range cliChatGroupPostRecords(t, transcript) {
		var line cliChatGroupLine
		if err := json.Unmarshal(raw, &line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	if len(lines) != 2 || postedIDs[0] == postedIDs[1] {
		t.Fatal("actual group transcript duplicated or lost a successful post")
	}
	for i, line := range lines {
		if line.ID != postedIDs[i] || line.Time.IsZero() || line.Sender != source || line.Kind != "bot" || line.Text != body {
			t.Fatal("actual five-field group line lost full public body, original bot sender, identity or time")
		}
	}
	cliChatGroupPostDelivery(t, srv.Root, source, b, group.ID, lines)
	cliChatGroupPostDelivery(t, srv.Root, source, c, group.ID, lines)
	cliChatGroupPostDelivery(t, srv.Root, source, source, group.ID, nil)
	if err := os.WriteFile(filepath.Join(srv.Root, "work", source, "group-overflow.txt"), []byte(body+"Z"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, denied := range []struct{ cue, command string }{
		{"CLI_CHAT_POST_MISSING_CALL", `env -u XBOT_CALL_ID "$XBOT_BIN" chat-group post ` + group.ID + ` --self --text 'Missing original call must not append' --json`},
		{"CLI_CHAT_POST_EMPTY", `"$XBOT_BIN" chat-group post ` + group.ID + ` --self --text '  ' --json`},
		{"CLI_CHAT_POST_OVERFLOW", `"$XBOT_BIN" chat-group post ` + group.ID + ` --self --file group-overflow.txt --json`},
	} {
		before, err := os.ReadFile(transcript)
		if err != nil {
			t.Fatal(err)
		}
		ratePath := filepath.Join(srv.Root, "peer-rate.json")
		rateBefore, err := os.ReadFile(ratePath)
		if err != nil {
			t.Fatal(err)
		}
		result := run(denied.cue, denied.command)
		exit, _, ok := strings.Cut(result, "\n")
		var reply cliChatGroupPostReply
		if !ok || !exitLine.MatchString(exit) || exit == "exit 0" || json.NewDecoder(strings.NewReader(commandOutput(result))).Decode(&reply) != nil || reply.ID != group.ID || reply.Appended || reply.MessageID != "" || len(reply.PostedMembers) != 0 || reply.FailedMember != "" || reply.Stage != "validation" || reply.Error == "" {
			t.Fatal("missing-call/empty/oversize post did not report its real pre-append refusal")
		}
		after, err := os.ReadFile(transcript)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("rejected post appended or rewrote group history")
		}
		rateAfter, err := os.ReadFile(ratePath)
		if err != nil || !bytes.Equal(rateBefore, rateAfter) {
			t.Fatal("pre-append validation refusal consumed or changed persistent rate state")
		}
	}
	for name, before := range beforeConfig {
		after, err := os.ReadFile(filepath.Join(srv.Root, "bots", name, "config.json"))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("group create/post changed an actual Bot configuration")
		}
	}
}

func TestCLIChatGroupRuntimePostFanoutPartial(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const source, b, c = "ChatPartialA", "ChatPartialB", "ChatPartialC"
	for _, name := range []string{source, b, c} {
		srv.CreateBot(name, "Observe honest sequential group append and recipient persistence.")
	}
	primaryRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var primary struct{ Name string }
	if err := json.Unmarshal(primaryRaw, &primary); err != nil || primary.Name == "" || primary.Name == source {
		t.Fatal("partial fanout fixture must use an actual nonprimary source")
	}
	run := func(cue, command string) string {
		return cliChatGroupPostRun(t, srv, model, source, cue, command)
	}
	var created cliChatGroupCreateReply
	cliBotDecode(t, run("CLI_CHAT_PARTIAL_CREATE", `"$XBOT_BIN" chat-group create 'Actual partial fanout' --self --member `+c+` --member `+b+` --json`), &created)
	if !created.Created || created.Group == nil || created.Error != "" || !slices.Equal(created.Group.Members, []string{source, b, c}) {
		t.Fatal("actual source failed to create the lexicographic three-member fanout fixture")
	}
	group := *created.Group
	// The source has completed a real warm/create wake. C stays cold. A complete
	// bad JSONL record forces original Inbox.Open to fail, rather than using an
	// ignored incomplete tail, injected callback, mock receipt or fake context.
	badPath := filepath.Join(srv.Root, "bots", c, "inbox.jsonl")
	badBefore, err := os.ReadFile(badPath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	bad := append(bytes.Clone(badBefore), []byte("BADINBOX\n")...)
	if err := os.WriteFile(badPath, bad, 0600); err != nil {
		t.Fatal(err)
	}
	const text, cue = "Public partial fanout body.\nOnly this group text is delivered.", "CLI_CHAT_PARTIAL_POST"
	result := run(cue, `"$XBOT_BIN" chat-group post `+group.ID+` --self --text `+shellQuote(text)+` --json`)
	exit, _, ok := strings.Cut(result, "\n")
	var reply cliChatGroupPostReply
	if !ok || !exitLine.MatchString(exit) || exit == "exit 0" || json.NewDecoder(strings.NewReader(commandOutput(result))).Decode(&reply) != nil {
		t.Fatal("actual late fanout failure did not output its typed receipt before nonzero exit")
	}
	if reply.ID != group.ID || !reply.Appended || reply.MessageID == "" || !slices.Equal(reply.PostedMembers, []string{b}) || reply.FailedMember != c || reply.Stage != "recipient_open" || reply.Error == "" {
		t.Fatal("actual late failure hid the committed group line/B event or fabricated C completion")
	}
	records := cliChatGroupPostRecords(t, filepath.Join(srv.Root, "groups", group.ID, "transcript.jsonl"))
	if len(records) != 1 {
		t.Fatal("partial fanout rolled back or automatically retried the actual group append")
	}
	var line cliChatGroupLine
	if err := json.Unmarshal(records[0], &line); err != nil || line.ID != reply.MessageID || line.Time.IsZero() || line.Sender != source || line.Kind != "bot" || line.Text != text {
		t.Fatal("partial ACK does not describe the actual full five-field group record")
	}
	cliChatGroupPostDelivery(t, srv.Root, source, b, group.ID, []cliChatGroupLine{line})
	cliChatGroupPostDelivery(t, srv.Root, source, source, group.ID, nil)
	afterBad, err := os.ReadFile(badPath)
	if err != nil || !bytes.Equal(afterBad, bad) {
		t.Fatal("failed C open appended an event or rewrote the complete corrupt fixture")
	}
	// The persistent rate hit survives the later recipient failure; no direct
	// edits, reset, synthetic clock or extra post is used to infer that fact.
	rateRaw, err := os.ReadFile(filepath.Join(srv.Root, "peer-rate.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rate struct{ Hits map[string][]int64 }
	if err := json.Unmarshal(rateRaw, &rate); err != nil || len(rate.Hits["bot:"+source+"->group:"+group.ID]) != 1 {
		t.Fatal("late fanout failure reset or duplicated the actual persistent rate hit")
	}
}
