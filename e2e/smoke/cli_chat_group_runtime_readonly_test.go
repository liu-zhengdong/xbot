//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"xbot/internal/peer"
)

func TestCLIChatGroupRuntimeReadOnlyAndSchemas(t *testing.T) {
	cliSkillIsolation(t)
	model := &mcpRuntimeModel{scripts: map[string]*mcpRuntimeScript{}}
	var mu sync.Mutex
	retired := false
	model.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad scripted input", 400)
			return
		}
		var request stubRequest
		if json.Unmarshal(raw, &request) != nil {
			http.Error(w, "bad scripted JSON", 400)
			return
		}
		caller, _ := classify(request)
		if caller != callerCompaction && caller != callerExtract {
			mu.Lock()
			for _, name := range []string{"list_groups", "get_group_message_history", "create_group", "post_group"} {
				retired = retired || bytes.Contains(raw, []byte(name))
			}
			mu.Unlock()
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		model.handle(w, r)
	}))
	t.Cleanup(model.server.Close)
	srv := Start(t, &Stub{srv: model.server})
	const source, recipient = "ChatGateSource", "ChatGatePeer"
	srv.CreateBot(source, "Nonprimary source for chat-group readonly acceptance.")
	srv.CreateBot(recipient, "Actual recipient of the later dev source post.")
	primaryPath := filepath.Join(srv.Root, "primary.json")
	primaryRaw, err := os.ReadFile(primaryPath)
	if err != nil {
		t.Fatal(err)
	}
	var primary struct{ Name string }
	if err := json.Unmarshal(primaryRaw, &primary); err != nil || primary.Name == "" || primary.Name == source {
		t.Fatal("chat-group readonly fixture must use an actual nonprimary source")
	}
	// Valid original Bus metadata before any source Resource capture. No owner
	// config changes, owner HTTP group route or fabricated callback is used.
	group, err := (&peer.Bus{Root: srv.Root}).CreateGroup(source, "Readonly gate room", []string{recipient})
	if err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(srv.Root, "groups", group.ID, "transcript.jsonl")
	historical := cliChatGroupLine{ID: "gm_gate_fixture", Time: time.Now().UTC().Add(-time.Hour), Sender: recipient, Kind: "bot", Text: "HISTORICAL_GATE_GROUP_BODY"}
	raw, err := json.Marshal(map[string]any{"id": historical.ID, "time": historical.Time, "sender": historical.Sender, "kind": historical.Kind, "text": historical.Text})
	if err != nil {
		t.Fatal(err)
	}
	// One complete historical JSONL row is data only. Runtime posting and its
	// authored event below must come from the actual dev Shell Job call.
	file, err := os.OpenFile(transcript, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(append(raw, '\n')); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(srv.Root, "work", source)
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "chat-native-read-proof.txt"), []byte("CHAT_GROUP_NATIVE_FILE_TOOL_PRESERVED"), 0600); err != nil {
		t.Fatal(err)
	}
	list := `"$XBOT_BIN" chat-group list --self --json`
	history := `"$XBOT_BIN" chat-group history ` + group.ID + ` --self --json`
	const mainCue, research, dev = "CLI_CHAT_GATE_MAIN", "CLI_CHAT_GATE_RESEARCH", "CLI_CHAT_GATE_DEV"
	model.add(mainCue,
		mcpRuntimeTool("sh", map[string]string{"description": "List current-member groups from the actual nonprimary source", "command": list}),
		mcpRuntimeTool("sh", map[string]string{"description": "Read the complete untrusted historical group row", "command": history}),
		mcpRuntimeTool("tools", map[string]string{"action": "describe", "id": "recall_history"}),
	)
	cliSkillSend(t, srv, source, mainCue)
	checkList := func(result string) {
		t.Helper()
		var reply cliChatGroupListReply
		cliBotDecode(t, result, &reply)
		if reply.Groups == nil || reply.Count != 1 || len(*reply.Groups) != 1 || (*reply.Groups)[0].ID != group.ID || !slices.Equal((*reply.Groups)[0].Members, group.Members) || reply.File != "" || reply.Error != "" {
			t.Fatal("source group List lost its actual complete member group")
		}
	}
	checkHistory := func(result string) {
		t.Helper()
		var reply cliChatGroupHistoryReply
		cliBotDecode(t, result, &reply)
		if reply.Group == nil || reply.Group.ID != group.ID || reply.Messages == nil || reply.Count != 1 || len(*reply.Messages) != 1 || !reflect.DeepEqual((*reply.Messages)[0], historical) || !reply.Untrusted || reply.File != "" || reply.Error != "" {
			t.Fatal("source History lost the complete real historical row or untrusted marker")
		}
	}
	mainResults := model.results(mainCue)
	checkList(mainResults[0])
	checkHistory(mainResults[1])
	if strings.HasPrefix(mainResults[2], "error:") || !strings.Contains(mainResults[2], "recall_history") {
		t.Fatal("the preserved Tools gate lost native history description")
	}

	type fileSnapshot struct {
		Exists bool
		Body   []byte
	}
	snapshot := func() map[string]fileSnapshot {
		t.Helper()
		files := map[string]fileSnapshot{}
		// Recipient inbox must stay untouched; source inbox may legitimately
		// receive the original dispatch completion notification.
		for _, path := range []string{primaryPath, filepath.Join(srv.Root, "bots", source, "config.json"), filepath.Join(srv.Root, "bots", recipient, "config.json"), filepath.Join(srv.Root, "bots", recipient, "inbox.jsonl"), filepath.Join(srv.Root, "peer-rate.json")} {
			raw, err := os.ReadFile(path)
			if os.IsNotExist(err) {
				files[path] = fileSnapshot{}
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			files[path] = fileSnapshot{Exists: true, Body: raw}
		}
		if err := filepath.WalkDir(filepath.Join(srv.Root, "groups"), func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[path] = fileSnapshot{Exists: true, Body: raw}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return files
	}
	beforeResearch := snapshot()
	injection := filepath.Join(workspace, "chat-group-operator-must-not-run")
	model.add(research,
		mcpRuntimeTool("sh", map[string]string{"description": "List actual member groups in research", "command": list}),
		mcpRuntimeTool("sh", map[string]string{"description": "Read actual member history in research", "command": history}),
		mcpRuntimeTool("sh", map[string]string{"description": "Read chat-group CLI help in research", "command": `"$XBOT_BIN" chat-group --help`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject group creation before the interpreter", "command": `"$XBOT_BIN" chat-group create MustNotCreate --self --member ` + recipient + ` --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject group posting before the interpreter", "command": `"$XBOT_BIN" chat-group post ` + group.ID + ` --self --text RESEARCH_MUST_NOT_POST --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject missing-self group commands before the interpreter", "command": `"$XBOT_BIN" chat-group list --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject shell operators attached to a readonly group list", "command": list + `; printf injected > ` + shellQuote(injection)}),
	)
	const researchDispatch = "CLI_CHAT_GATE_DISPATCH_RESEARCH"
	model.add(researchDispatch, mcpRuntimeTool("dispatch", map[string]string{"type": "research", "task": research}))
	cliSkillSend(t, srv, source, researchDispatch)
	WaitFor(t, 12*time.Second, "actual chat-group research steps", func() (bool, string) {
		results := model.results(research)
		return results[len(results)-1] != "", "research steps not completed"
	})
	r := model.results(research)
	checkList(r[0])
	checkHistory(r[1])
	if !strings.HasPrefix(r[2], "exit 0\n") || strings.TrimSpace(commandOutput(r[2])) == "" {
		t.Fatal("research did not allow actual chat-group help")
	}
	for _, rejected := range r[3:] {
		line, _, _ := strings.Cut(rejected, "\n")
		if exitLine.MatchString(line) || strings.TrimSpace(rejected) == "" || len(rejected) > 32<<10 {
			t.Fatal("research group write or pseudo-read reached the interpreter")
		}
	}
	if !reflect.DeepEqual(beforeResearch, snapshot()) {
		t.Fatal("rejected research commands changed actual group/meta/history/recipient inbox/pointer/config state")
	}
	if _, err := os.Stat(injection); !os.IsNotExist(err) {
		t.Fatal("rejected pseudo-read executed its shell operator")
	}

	// Dev runs only after exact refusal-state comparison. Post uses the native
	// Shell's real call identity, with no Call-ID, sender or chain injected here.
	const devText = "DEV_ACTUAL_GROUP_POST"
	model.add(dev,
		mcpRuntimeTool("sh", map[string]string{"description": "Post as this member through the actual dev source Shell Job", "command": `"$XBOT_BIN" chat-group post ` + group.ID + ` --self --text ` + devText + ` --json`}),
		mcpRuntimeTool("read_file", map[string]string{"path": "chat-native-read-proof.txt"}),
	)
	const devDispatch = "CLI_CHAT_GATE_DISPATCH_DEV"
	model.add(devDispatch, mcpRuntimeTool("dispatch", map[string]string{"type": "dev", "task": dev}))
	cliSkillSend(t, srv, source, devDispatch)
	WaitFor(t, 12*time.Second, "actual chat-group dev steps", func() (bool, string) {
		results := model.results(dev)
		return results[len(results)-1] != "", "dev steps not completed"
	})
	d := model.results(dev)
	var posted cliChatGroupPostReply
	cliBotDecode(t, d[0], &posted)
	if posted.ID != group.ID || posted.MessageID == "" || !posted.Appended || !slices.Equal(posted.PostedMembers, []string{recipient}) || posted.FailedMember != "" || posted.Stage != "complete" || posted.Error != "" {
		t.Fatal("actual dev source post lost its appended message and real recipient receipt")
	}
	if !strings.Contains(d[1], "CHAT_GROUP_NATIVE_FILE_TOOL_PRESERVED") {
		t.Fatal("actual dev source lost its preserved native file tool")
	}
	stored, err := os.ReadFile(transcript)
	if err != nil {
		t.Fatal(err)
	}
	rows := bytes.Split(bytes.TrimSpace(stored), []byte{'\n'})
	var actual cliChatGroupLine
	if len(rows) != 2 || json.Unmarshal(rows[1], &actual) != nil || actual.ID != posted.MessageID || actual.Sender != source || actual.Kind != "bot" || actual.Text != devText || actual.Time.IsZero() {
		t.Fatal("dev did not append exactly one actual Bot-authored group record")
	}
	inboxRaw, err := os.ReadFile(filepath.Join(srv.Root, "bots", recipient, "inbox.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	events := 0
	for _, row := range bytes.Split(bytes.TrimSpace(inboxRaw), []byte{'\n'}) {
		var record struct {
			Op    string
			Event struct {
				Source, Type, Sender, Content, Chain string
				SenderKind                           string `json:"sender_kind"`
				Untrusted                            bool
				Hops                                 int
			}
		}
		if err := json.Unmarshal(row, &record); err != nil {
			t.Fatal("actual recipient inbox contains incomplete JSON")
		}
		if record.Op == "post" && record.Event.Source == "group:"+group.ID {
			events++
			ev := record.Event
			if ev.Type != "message" || ev.Sender != source || ev.SenderKind != "bot" || !ev.Untrusted || ev.Chain == "" || ev.Hops != 1 || !strings.Contains(ev.Content, devText) {
				t.Fatal("actual dev post lost Bot authorship, untrusted body or original call-chain delivery")
			}
		}
	}
	if events != 1 {
		t.Fatalf("actual dev post recipient event count=%d", events)
	}
	model.mu.Lock()
	mainSeen := model.scripts[mainCue].callers[callerForeground]
	researchSeen := model.scripts[research].callers[callerSubagent]
	devSeen := model.scripts[dev].callers[callerSubagent]
	model.mu.Unlock()
	if !mainSeen || !researchSeen || !devSeen {
		t.Fatal("retirement checks did not reach fresh actual main and continuable requests")
	}
	mu.Lock()
	old := retired
	mu.Unlock()
	if old {
		t.Fatal("actual provider wire still exposes a retired group schema, index or instruction")
	}
}
