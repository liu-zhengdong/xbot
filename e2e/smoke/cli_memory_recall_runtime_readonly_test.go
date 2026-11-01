//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCLIMemoryRecallRuntimeInboxAndReadOnly(t *testing.T) {
	cliSkillIsolation(t)
	model := newCLIMemoryRecallModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const source, sender = "RecallInboxSource", "RecallInboxSender"
	srv.CreateBot(source, "Read only this source's visible memory using its original frozen sender context.")
	srv.CreateBot(sender, "Deliver one actual Bot-kind inbox message to the recall source.")
	primaryRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var primary struct{ Name string }
	if err := json.Unmarshal(primaryRaw, &primary); err != nil || primary.Name == "" || primary.Name == source {
		t.Fatal("recall acceptance must use an actual API nonprimary source")
	}
	configPath := filepath.Join(srv.Root, "bots", source, "config.json")
	configRaw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct{ Owners []string }
	if err := json.Unmarshal(configRaw, &cfg); err != nil || len(cfg.Owners) != 1 || cfg.Owners[0] == "" {
		t.Fatal("actual API source does not identify this run's authenticated owner")
	}
	owner := cfg.Owners[0]
	const otherPerson = "uRecallOtherOwner"
	if owner == otherPerson {
		t.Fatal("controlled other-person fixture collided with the authenticated owner")
	}
	const warm = "MEMORY_WARM_A11"
	model.base.add(warm, mcpRuntimeTool("sh", map[string]string{"description": "Warm the actual source with a native Shell before memory comparisons", "command": `printf 'recall-source-ready\n'`}))
	cliSkillSend(t, srv, source, warm)
	if !strings.HasPrefix(model.base.results(warm)[0], "exit 0\n") {
		t.Fatal("actual source did not complete its original warm wake")
	}
	botRoot := filepath.Join(srv.Root, "bots", source, "memory")
	personRoot := filepath.Join(srv.Root, "people", owner, "memory")
	otherRoot := filepath.Join(srv.Root, "people", otherPerson, "memory")
	const query = "RECALL_GATE_TOKEN"
	const botMarker, personMarker, otherMarker = "BOT_PRIVATE_VISIBLE", "OWNER_PRIVATE_VISIBLE", "OTHER_OWNER_MUST_NOT_LEAK"
	// Canonical dated memory data, not an injected Memory service or sender.
	for root, marker := range map[string]string{botRoot: botMarker, personRoot: personMarker, otherRoot: otherMarker} {
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, "memory.md")
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			raw = []byte("# memory\n\n")
		} else if err != nil {
			t.Fatal(err)
		}
		line := "\n- (" + time.Now().Format("2006-01-02") + ") " + query + " " + marker + "\n"
		if err := os.WriteFile(path, append(raw, []byte(line)...), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(botRoot, "literal-help.txt"), []byte("Literal --help BOT_LITERAL_HELP_RESULT\n"), 0600); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(srv.Root, "work", source)
	if err := os.WriteFile(filepath.Join(workspace, "recall-native-read-proof.txt"), []byte("RECALL_NATIVE_FILE_TOOL_PRESERVED"), 0600); err != nil {
		t.Fatal(err)
	}
	decode := func(result string) cliMemoryRecallReply {
		t.Helper()
		var reply cliMemoryRecallReply
		cliBotDecode(t, result, &reply)
		if reply.Error != "" || reply.File != "" || len(commandOutput(result)) > 32<<10 {
			t.Fatal("small recall did not return its complete original text through native Shell")
		}
		return reply
	}
	recall := `"$XBOT_BIN" memory-recall --self --query ` + query + ` --json`
	personRecall := `"$XBOT_BIN" memory-recall --self --query ` + query + ` --scope person --json`
	const ownerCue, inboxCue = "MEMORY_OWNER_B22", "MEMORY_BOT_INBOX_C33"
	model.base.add(ownerCue,
		mcpRuntimeTool("sh", map[string]string{"description": "Observe the actual owner's default Bot-and-person union", "command": recall}),
		mcpRuntimeTool("sh", map[string]string{"description": "Validate the authenticated owner's actual person directory", "command": personRecall}),
		mcpRuntimeTool("tools", map[string]string{"action": "describe", "id": "detail"}),
	)
	cliSkillSend(t, srv, source, ownerCue)
	o := model.base.results(ownerCue)
	union, person := decode(o[0]), decode(o[1])
	if !strings.Contains(union.Text, botMarker) || !strings.Contains(union.Text, personMarker) || strings.Contains(union.Text, otherMarker) || !strings.Contains(person.Text, "person:memory.md:") || !strings.Contains(person.Text, personMarker) || strings.Contains(person.Text, botMarker) || strings.Contains(person.Text, otherMarker) {
		t.Fatal("actual owner context did not select only its Bot/person memory directories")
	}
	if strings.HasPrefix(o[2], "error:") || !strings.Contains(o[2], "detail") {
		t.Fatal("the preserved Context Tools gate lost live detail")
	}

	// This request originates solely from an actual KindBot recipient event.
	// No owner SSE call or fabricated context is used to execute these steps.
	model.base.add(inboxCue,
		mcpRuntimeTool("sh", map[string]string{"description": "Recall the default scope from a real Bot-kind inbox turn", "command": recall}),
		mcpRuntimeTool("sh", map[string]string{"description": "Observe that Bot-kind has no person even with shared owners", "command": personRecall}),
		mcpRuntimeTool("sh", map[string]string{"description": "Preserve empty-query no-token behavior in the same Bot turn", "command": `"$XBOT_BIN" memory-recall --self --json`}),
	)
	const sendCue = "MEMORY_DELIVERY_D44"
	model.base.add(sendCue, mcpRuntimeTool("sh", map[string]string{"description": "Deliver the recall cue through the original local peer-message writer", "command": `"$XBOT_BIN" peer-message ` + source + ` --self --text ` + inboxCue + ` --json`}))
	cliSkillSend(t, srv, sender, sendCue)
	var delivered cliPeerMessageReply
	cliBotDecode(t, model.base.results(sendCue)[0], &delivered)
	if !delivered.Posted || delivered.Bot != source || delivered.Route != "local" || delivered.EventID == "" || delivered.Error != "" {
		t.Fatal("actual sender did not post the Bot-kind recall request")
	}
	WaitFor(t, 12*time.Second, "actual KindBot inbox recall steps", func() (bool, string) {
		results := model.base.results(inboxCue)
		return results[len(results)-1] != "", "recipient has not completed its actual recall steps"
	})
	b := model.base.results(inboxCue)
	botOnly, noPerson, noTokens := decode(b[0]), decode(b[1]), decode(b[2])
	if !strings.Contains(botOnly.Text, "bot:memory.md:") || !strings.Contains(botOnly.Text, botMarker) || strings.Contains(botOnly.Text, personMarker) || strings.Contains(botOnly.Text, otherMarker) || strings.Contains(botOnly.Text, "person:") || noPerson.Text != "no matches" || noTokens.Text != "no matches" {
		t.Fatal("actual KindBot frozen turn borrowed owner/person memory or changed no-token semantics")
	}
	inboxPath := filepath.Join(srv.Root, "bots", source, "inbox.jsonl")
	WaitFor(t, 12*time.Second, "original recipient completion before owner research dispatch", func() (bool, string) {
		raw, err := os.ReadFile(inboxPath)
		if err != nil {
			return false, "recipient inbox is not readable yet"
		}
		posted, acked := false, false
		for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte{'\n'}) {
			var record struct {
				Op, ID string
				Event  struct {
					ID, Source, Sender, Content string
					SenderKind                  string `json:"sender_kind"`
					Untrusted                   bool
				}
			}
			if json.Unmarshal(line, &record) != nil {
				return false, "recipient inbox append has not completed"
			}
			if record.Op == "post" && record.Event.ID == delivered.EventID {
				ev := record.Event
				posted = ev.Source == "bot:"+sender && ev.Sender == sender && ev.SenderKind == "bot" && ev.Untrusted && ev.Content == inboxCue
			}
			acked = acked || record.Op == "ack" && record.ID == delivered.EventID
		}
		return posted && acked, "actual Bot event has not completed through the original serve consumer"
	})

	type fileSnapshot struct {
		Exists bool
		Body   []byte
	}
	snapshot := func() map[string]fileSnapshot {
		t.Helper()
		files := map[string]fileSnapshot{}
		config, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatal(err)
		}
		files[configPath] = fileSnapshot{Exists: true, Body: config}
		for _, root := range []string{botRoot, personRoot, otherRoot, filepath.Join(workspace, "results")} {
			if _, err := os.Stat(root); os.IsNotExist(err) {
				files[root] = fileSnapshot{}
				continue
			} else if err != nil {
				t.Fatal(err)
			}
			files[root] = fileSnapshot{Exists: true}
			if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
				if err != nil || entry.IsDir() || entry.Name() == ".lock" {
					return err
				}
				raw, err := os.ReadFile(path)
				if err == nil {
					files[path] = fileSnapshot{Exists: true, Body: raw}
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
		}
		return files
	}
	beforeResearch := snapshot()
	const research, dispatchCue = "MEMORY_RESEARCH_E55", "MEMORY_DISPATCH_F66"
	injection := filepath.Join(workspace, "memory-operator-must-not-run")
	model.base.add(research,
		mcpRuntimeTool("sh", map[string]string{"description": "Recall Bot memory in actual research without inferring its person policy", "command": `"$XBOT_BIN" memory-recall --self --scope bot --query ` + query + ` --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Treat literal help query text as a real memory search", "command": `"$XBOT_BIN" memory-recall --self --scope bot --query --help --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Read memory-recall help in research", "command": `"$XBOT_BIN" memory-recall --help`}),
		mcpRuntimeTool("read_file", map[string]string{"path": "recall-native-read-proof.txt"}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject shell operators attached to a readonly memory query", "command": recall + `; printf injected > ` + shellQuote(injection)}),
	)
	model.base.add(dispatchCue, mcpRuntimeTool("dispatch", map[string]string{"type": "research", "task": research}))
	cliSkillSend(t, srv, source, dispatchCue)
	WaitFor(t, 12*time.Second, "actual memory-recall research steps", func() (bool, string) {
		results := model.base.results(research)
		return results[len(results)-1] != "", "research steps not completed"
	})
	r := model.base.results(research)
	read, literal := decode(r[0]), decode(r[1])
	if !strings.Contains(read.Text, botMarker) || strings.Contains(read.Text, personMarker) || strings.Contains(read.Text, otherMarker) || !strings.Contains(literal.Text, "bot:literal-help.txt:1") || !strings.Contains(literal.Text, "BOT_LITERAL_HELP_RESULT") {
		t.Fatal("research lost actual Bot recall or treated literal query help as CLI help")
	}
	if !strings.HasPrefix(r[2], "exit 0\n") || strings.TrimSpace(commandOutput(r[2])) == "" || !strings.Contains(r[3], "RECALL_NATIVE_FILE_TOOL_PRESERVED") {
		t.Fatal("research help or preserved native file reading regressed")
	}
	line, _, _ := strings.Cut(r[4], "\n")
	if exitLine.MatchString(line) || strings.TrimSpace(r[4]) == "" || len(r[4]) > 32<<10 {
		t.Fatal("research pseudo-read reached the interpreter")
	}
	if !reflect.DeepEqual(beforeResearch, snapshot()) {
		t.Fatal("readonly research changed actual memory/config/result artifacts beyond original scope locks")
	}
	if _, err := os.Stat(injection); !os.IsNotExist(err) {
		t.Fatal("rejected pseudo-read executed its shell operator")
	}
	model.base.mu.Lock()
	ownerSeen := model.base.scripts[ownerCue].callers[callerForeground]
	inboxSeen := model.base.scripts[inboxCue].callers[callerForeground]
	researchSeen := model.base.scripts[research].callers[callerSubagent]
	model.base.mu.Unlock()
	if !ownerSeen || !inboxSeen || !researchSeen {
		t.Fatal("recall checks did not reach real owner, Bot-inbox and continuable research requests")
	}
	model.mu.Lock()
	retired, botWireSeen := false, false
	for _, raw := range model.raw {
		var request stubRequest
		if json.Unmarshal(raw, &request) != nil {
			continue
		}
		if strings.Contains(lastUser(request), inboxCue) && strings.Contains(lastUser(request), `source="bot:`+sender+`"`) {
			botWireSeen = true
		}
		var wire struct {
			Tools []struct{ Function struct{ Name string } }
		}
		if json.Unmarshal(raw, &wire) != nil {
			continue
		}
		for _, tool := range wire.Tools {
			retired = retired || tool.Function.Name == "recall"
		}
		for _, message := range request.Messages {
			// Match the old exact native index/guide spellings; the current noun
			// memory-recall and preserved recall_history legitimately share text.
			for _, needle := range []string{"recall [memory]", "更早的记忆用 recall 查", "`recall`"} {
				retired = retired || strings.Contains(message.Content, needle)
			}
		}
	}
	model.mu.Unlock()
	if !botWireSeen || retired {
		t.Fatal("actual Bot source wire was absent or still exposed the retired native recall door")
	}
}
