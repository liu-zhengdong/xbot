//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"xbot/internal/peer"
)

func TestCLIChatGroupRuntimeMemberListHistory(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const source, shared, other, otherPeer, empty = "ChatReadSource", "ChatReadShared", "ChatReadOther", "ChatReadOtherPeer", "ChatReadEmpty"
	for _, name := range []string{source, shared, other, otherPeer, empty} {
		srv.CreateBot(name, "Read only actual current-member chat groups and complete untrusted history.")
	}
	readConfig := func(name string) map[string]json.RawMessage {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(srv.Root, "bots", name, "config.json"))
		if err != nil {
			t.Fatal(err)
		}
		var cfg map[string]json.RawMessage
		if err := json.Unmarshal(raw, &cfg); err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	var owners []string
	if err := json.Unmarshal(readConfig(source)["owners"], &owners); err != nil || len(owners) == 0 || owners[0] == "" {
		t.Fatal("actual source has no authenticated owner fixture")
	}
	const extraOwner = "chat-read-shared-extra-owner"
	if slices.Contains(owners, extraOwner) {
		t.Fatal("controlled extra owner collides with this run's actual ownership")
	}
	// Source member scope deliberately differs from the Web all-members-owner
	// view. These are persisted ownership data, never fake sender identities.
	for name, nextOwners := range map[string][]string{source: append(slices.Clone(owners), extraOwner), shared: {extraOwner}} {
		cfg := readConfig(name)
		raw, err := json.Marshal(nextOwners)
		if err != nil {
			t.Fatal(err)
		}
		cfg["owners"] = raw
		encoded, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(srv.Root, "bots", name, "config.json"), encoded, 0600); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Date(2026, 11, 2, 1, 2, 3, 456000000, time.UTC)
	bus := &peer.Bus{Root: srv.Root, Now: func() time.Time { return at }}
	memberGroup, err := bus.CreateGroup(source, "Source\tmember room", []string{shared})
	if err != nil {
		t.Fatal(err)
	}
	emptyGroup, err := bus.CreateGroup(source, "Empty source member", []string{shared})
	if err != nil {
		t.Fatal(err)
	}
	foreignGroup, err := bus.CreateGroup(other, "Other members only", []string{otherPeer})
	if err != nil {
		t.Fatal(err)
	}
	var lines []cliChatGroupLine
	var log bytes.Buffer
	for i := 0; i < 60; i++ {
		sender := source
		if i%2 != 0 {
			sender = shared
		}
		text := fmt.Sprintf("group body %02d", i)
		if i%5 == 0 {
			text += " NeEdLe"
		}
		if i == 45 {
			text += "\nFAKE_ROW\tquoted \"data\""
		}
		line := cliChatGroupLine{ID: fmt.Sprintf("gm_fixture_%03d", i), Time: at.Add(time.Duration(i) * time.Second), Sender: sender, Kind: "bot", Text: text}
		// Complete historical JSONL data fixture; it is not a claimed runtime
		// post and does not enqueue artificial incoming events or wake consumers.
		raw, err := json.Marshal(struct {
			ID     string    `json:"id"`
			Time   time.Time `json:"time"`
			Sender string    `json:"sender"`
			Kind   string    `json:"kind"`
			Text   string    `json:"text"`
		}{line.ID, line.Time, line.Sender, line.Kind, line.Text})
		if err != nil {
			t.Fatal(err)
		}
		log.Write(raw)
		log.WriteByte('\n')
		lines = append(lines, line)
	}
	path := filepath.Join(srv.Root, "groups", memberGroup.ID, "transcript.jsonl")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(log.Bytes()); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	// Reload the actual existing private serve before any source CLI use, so
	// source ownership is captured from the completed persistent fixture.
	srv.Restart()
	status, body := cliFullAPI(t, srv, http.MethodGet, "/api/groups", nil)
	var web struct{ Groups []cliChatGroup }
	if status != http.StatusOK || json.Unmarshal(body, &web) != nil {
		t.Fatal("actual owner group-view comparison failed")
	}
	webOther := false
	for _, g := range web.Groups {
		if g.ID == memberGroup.ID || g.ID == emptyGroup.ID {
			t.Fatal("fixture did not distinguish source membership from Web all-member ownership")
		}
		webOther = webOther || g.ID == foreignGroup.ID
	}
	if !webOther {
		t.Fatal("actual Web owner view lost its owned non-source group fixture")
	}
	run := func(name, cue, command string) string {
		t.Helper()
		model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Read original source current-member groups and full filtered history", "command": command}))
		cliSkillSend(t, srv, name, cue)
		results := model.results(cue)
		if len(results) != 1 || len(commandOutput(results[0])) > 32<<10 {
			t.Fatal("actual group source did not return one bounded Shell result")
		}
		return results[0]
	}
	groupValue := func(g peer.Group) cliChatGroup {
		return cliChatGroup{ID: g.ID, Name: g.Name, Members: g.Members, Created: g.Created}
	}
	var listed cliChatGroupListReply
	cliBotDecode(t, run(source, "CLI_CHAT_READ_LIST", `XBOT_BOT=`+other+` "$XBOT_BIN" chat-group list --self --json`), &listed)
	if listed.Groups == nil || listed.Count != 2 || len(*listed.Groups) != 2 || listed.File != "" || listed.Error != "" {
		t.Fatal("list did not return complete current-member scope")
	}
	wantGroups := []cliChatGroup{groupValue(emptyGroup), groupValue(memberGroup)}
	if !reflect.DeepEqual(*listed.Groups, wantGroups) {
		t.Fatal("Group4 fields/order/membership were lost or owner/diagnostic view replaced source")
	}
	checkHistory := func(cue, args string, want []cliChatGroupLine) {
		t.Helper()
		var reply cliChatGroupHistoryReply
		cliBotDecode(t, run(source, cue, `"$XBOT_BIN" chat-group history `+memberGroup.ID+` --self `+args+` --json`), &reply)
		if reply.Group == nil || !reflect.DeepEqual(*reply.Group, groupValue(memberGroup)) || reply.Messages == nil || reply.Count != len(want) || !reply.Untrusted || reply.File != "" || reply.Error != "" || !reflect.DeepEqual(*reply.Messages, want) {
			t.Fatal("history lost Group4/Line5, exact selection order, or untrusted data status")
		}
	}
	checkHistory("CLI_CHAT_READ_DEFAULT", "", lines[40:])
	checkHistory("CLI_CHAT_READ_CAP", "--limit 99", lines[10:])
	checkHistory("CLI_CHAT_READ_FILTER", "--before-id "+lines[55].ID+" --limit 3 --sender "+shared+" --keyword needle", []cliChatGroupLine{lines[25], lines[35], lines[45]})
	human := run(source, "CLI_CHAT_READ_QUOTE", `"$XBOT_BIN" chat-group history `+memberGroup.ID+` --self --before-id `+lines[55].ID+` --limit 3 --sender `+shared+` --keyword needle`)
	if !strings.HasPrefix(human, "exit 0\n") || !strings.Contains(commandOutput(human), strconv.Quote(lines[45].Text)) || strings.Contains(commandOutput(human), "\nFAKE_ROW\t") {
		t.Fatal("actual human history did not quote untrusted multiline text as one record")
	}
	var emptyHistory cliChatGroupHistoryReply
	cliBotDecode(t, run(source, "CLI_CHAT_READ_EMPTY_HISTORY", `"$XBOT_BIN" chat-group history `+emptyGroup.ID+` --self --json`), &emptyHistory)
	if emptyHistory.Group == nil || emptyHistory.Group.ID != emptyGroup.ID || emptyHistory.Messages == nil || len(*emptyHistory.Messages) != 0 || emptyHistory.Count != 0 || !emptyHistory.Untrusted || emptyHistory.Error != "" {
		t.Fatal("empty member history lost complete metadata and actual empty array")
	}
	denied := run(source, "CLI_CHAT_READ_FOREIGN_DENY", `"$XBOT_BIN" chat-group history `+foreignGroup.ID+` --self --json`)
	line, _, ok := strings.Cut(denied, "\n")
	if !ok || !exitLine.MatchString(line) || line == "exit 0" {
		t.Fatal("source could read a group where only the Web owner had access")
	}
	var refusal cliChatGroupHistoryReply
	if err := json.NewDecoder(strings.NewReader(commandOutput(denied))).Decode(&refusal); err != nil || refusal.Error == "" || refusal.Group != nil || refusal.Messages != nil || refusal.Count != 0 {
		t.Fatal("nonmember refusal exposed group metadata or messages")
	}
	var emptyList cliChatGroupListReply
	cliBotDecode(t, run(empty, "CLI_CHAT_READ_EMPTY_LIST", `"$XBOT_BIN" chat-group list --self --json`), &emptyList)
	if emptyList.Groups == nil || len(*emptyList.Groups) != 0 || emptyList.Count != 0 || emptyList.File != "" || emptyList.Error != "" {
		t.Fatal("nonmember source inherited its owner's groups instead of actual empty array")
	}
}
