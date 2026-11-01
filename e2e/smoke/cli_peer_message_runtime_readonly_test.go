//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCLIPeerMessageRuntimeReadOnlyAndSchemas(t *testing.T) {
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
			retired = retired || bytes.Contains(raw, []byte("message_bot"))
			mu.Unlock()
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		model.handle(w, r)
	}))
	t.Cleanup(model.server.Close)
	srv := Start(t, &Stub{srv: model.server})
	const source, target = "PeerReadonlySource", "PeerReadonlyTarget"
	srv.CreateBot(source, "Actual nonprimary source for peer-message readonly acceptance.")
	srv.CreateBot(target, "Actual local recipient for the two permitted source sends.")
	primaryPath := filepath.Join(srv.Root, "primary.json")
	primaryRaw, err := os.ReadFile(primaryPath)
	if err != nil {
		t.Fatal(err)
	}
	var primary struct{ Name string }
	if err := json.Unmarshal(primaryRaw, &primary); err != nil || primary.Name == "" || primary.Name == source {
		t.Fatal("peer-message readonly fixture must use an actual nonprimary source")
	}
	workspace := filepath.Join(srv.Root, "work", source)
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "peer-native-read-proof.txt"), []byte("PEER_NATIVE_FILE_TOOL_PRESERVED"), 0600); err != nil {
		t.Fatal(err)
	}
	inboxPath := filepath.Join(srv.Root, "bots", target, "inbox.jsonl")
	type inboxRecord struct {
		Op, ID string
		Event  struct {
			ID, Source, Type, Sender, Content, Chain string
			SenderKind                               string `json:"sender_kind"`
			Untrusted                                bool
			Hops                                     int
			Time                                     time.Time
		}
	}
	readInbox := func() []inboxRecord {
		t.Helper()
		raw, err := os.ReadFile(inboxPath)
		if err != nil {
			t.Fatal(err)
		}
		var records []inboxRecord
		for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte{'\n'}) {
			var record inboxRecord
			if err := json.Unmarshal(line, &record); err != nil {
				t.Fatal("actual recipient inbox contains incomplete JSON")
			}
			records = append(records, record)
		}
		return records
	}
	checkSend := func(result, text string) string {
		t.Helper()
		var reply cliPeerMessageReply
		cliBotDecode(t, result, &reply)
		if reply.Bot != target || reply.Route != "local" || !reply.Posted || reply.EventID == "" || reply.Stage != "complete" || reply.Error != "" || reply.EnvelopeID != "" || reply.Gateway != "" || reply.GatewayStatus != 0 || reply.Queued || reply.Recorded != nil || reply.RecordSeq != 0 || len(commandOutput(result)) > 32<<10 {
			t.Fatal("actual local send lost its short posted receipt or invented remote/record facts")
		}
		matches := 0
		for _, record := range readInbox() {
			if record.Op != "post" || record.Event.ID != reply.EventID {
				continue
			}
			matches++
			ev := record.Event
			if ev.Source != "bot:"+source || ev.Type != "message" || ev.Sender != source || ev.SenderKind != "bot" || !ev.Untrusted || ev.Content != text || ev.Chain != "ch_"+source || ev.Hops != 1 || ev.Time.IsZero() {
				t.Fatal("actual local event lost Bot identity, untrusted body or original source chain/hops")
			}
		}
		if matches != 1 {
			t.Fatalf("actual receipt EventID recipient post count=%d", matches)
		}
		return reply.EventID
	}
	// Cue strings are pairwise non-containing; the scripted model selects tasks
	// by literal inclusion and must never confuse a dispatch cue with its child.
	const mainCue, research, dev = "PEER_RO_MAIN_A21", "PEER_RO_RESEARCH_B43", "PEER_RO_DEV_C65"
	const mainText, devText = "PEER_MAIN_ALLOWED_BODY", "PEER_DEV_ALLOWED_BODY"
	model.add(mainCue,
		mcpRuntimeTool("sh", map[string]string{"description": "Send locally from the actual nonprimary source Shell", "command": `"$XBOT_BIN" peer-message ` + target + ` --self --text ` + mainText + ` --json`}),
		mcpRuntimeTool("tools", map[string]string{"action": "describe", "id": "recall_history"}),
	)
	cliSkillSend(t, srv, source, mainCue)
	mainResults := model.results(mainCue)
	mainEvent := checkSend(mainResults[0], mainText)
	if strings.HasPrefix(mainResults[1], "error:") || !strings.Contains(mainResults[1], "recall_history") {
		t.Fatal("the preserved Tools gate lost native history description")
	}
	// Let the existing serve consumer finish its first accepted delivery before
	// taking an exact inbox baseline; its normal ACK is not a research side effect.
	WaitFor(t, 12*time.Second, "original local recipient acknowledgment", func() (bool, string) {
		for _, record := range readInbox() {
			if record.Op == "ack" && record.ID == mainEvent {
				return true, ""
			}
		}
		return false, "recipient has not finished its original accepted event"
	})
	snapshot := func() map[string][]byte {
		t.Helper()
		files := map[string][]byte{}
		// Source inbox legitimately receives dispatch completions; recipient
		// inbox, rate, configs and the root Primary pointer must remain exact.
		for _, path := range []string{inboxPath, filepath.Join(srv.Root, "peer-rate.json"), primaryPath, filepath.Join(srv.Root, "bots", source, "config.json"), filepath.Join(srv.Root, "bots", target, "config.json")} {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			files[path] = raw
		}
		return files
	}
	beforeResearch := snapshot()
	injection := filepath.Join(workspace, "peer-operator-must-not-run")
	model.add(research,
		mcpRuntimeTool("sh", map[string]string{"description": "Read peer-message CLI help in research", "command": `"$XBOT_BIN" peer-message --help`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject an ordinary peer send before the interpreter", "command": `"$XBOT_BIN" peer-message ` + target + ` --self --text RESEARCH_MUST_NOT_SEND --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject literal help text as a write rather than a help request", "command": `"$XBOT_BIN" peer-message ` + target + ` --self --text --help --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject a literal help target rather than granting help permission", "command": `"$XBOT_BIN" peer-message --self --text TARGET_LITERAL_MUST_NOT_SEND --json -- --help`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject a missing-self send before the interpreter", "command": `"$XBOT_BIN" peer-message ` + target + ` --text MISSING_SELF_MUST_NOT_SEND --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject shell operators attached to peer-message help", "command": `"$XBOT_BIN" peer-message --help; printf injected > ` + shellQuote(injection)}),
	)
	const researchDispatch = "PEER_DISPATCH_RESEARCH_D87"
	model.add(researchDispatch, mcpRuntimeTool("dispatch", map[string]string{"type": "research", "task": research}))
	cliSkillSend(t, srv, source, researchDispatch)
	WaitFor(t, 12*time.Second, "actual peer-message research steps", func() (bool, string) {
		results := model.results(research)
		return results[len(results)-1] != "", "research steps not completed"
	})
	r := model.results(research)
	if !strings.HasPrefix(r[0], "exit 0\n") || strings.TrimSpace(commandOutput(r[0])) == "" || len(commandOutput(r[0])) > 32<<10 {
		t.Fatal("research did not permit bounded actual peer-message help")
	}
	for _, rejected := range r[1:] {
		line, _, _ := strings.Cut(rejected, "\n")
		if exitLine.MatchString(line) || strings.TrimSpace(rejected) == "" || len(rejected) > 32<<10 {
			t.Fatal("research send, literal-help write or pseudo-help reached the interpreter")
		}
	}
	if !reflect.DeepEqual(beforeResearch, snapshot()) {
		t.Fatal("rejected research commands changed actual recipient inbox/rate/config/Primary state")
	}
	if _, err := os.Stat(injection); !os.IsNotExist(err) {
		t.Fatal("rejected pseudo-help executed its shell operator")
	}

	// Actual dev Shell supplies its original Call-ID automatically. It starts
	// after the exact refusal comparison and cannot conceal a forbidden send.
	model.add(dev,
		mcpRuntimeTool("sh", map[string]string{"description": "Send through the actual same-source dev Shell call", "command": `"$XBOT_BIN" peer-message ` + target + ` --self --text ` + devText + ` --json`}),
		mcpRuntimeTool("read_file", map[string]string{"path": "peer-native-read-proof.txt"}),
	)
	const devDispatch = "PEER_DISPATCH_DEV_F09"
	model.add(devDispatch, mcpRuntimeTool("dispatch", map[string]string{"type": "dev", "task": dev}))
	cliSkillSend(t, srv, source, devDispatch)
	WaitFor(t, 12*time.Second, "actual peer-message dev steps", func() (bool, string) {
		results := model.results(dev)
		return results[len(results)-1] != "", "dev steps not completed"
	})
	d := model.results(dev)
	devEvent := checkSend(d[0], devText)
	if devEvent == mainEvent || !strings.Contains(d[1], "PEER_NATIVE_FILE_TOOL_PRESERVED") {
		t.Fatal("dev did not deliver a new original local event or lost its native file tool")
	}
	posts := 0
	for _, record := range readInbox() {
		if record.Op == "post" && record.Event.Source == "bot:"+source {
			posts++
		}
	}
	if posts != 2 {
		t.Fatalf("original local source recipient post count=%d, want two allowed sends", posts)
	}
	status, transcriptRaw := srv.do(http.MethodGet, "/api/bots/"+source+"/transcript", nil)
	if status != http.StatusOK {
		t.Fatalf("read actual source transcript: HTTP %d", status)
	}
	var transcript struct{ Messages []Row }
	if err := json.Unmarshal(transcriptRaw, &transcript); err != nil {
		t.Fatal(err)
	}
	for _, row := range transcript.Messages {
		if row.Internal && row.Role == "user" && strings.HasPrefix(row.Content, "<peer_message_outgoing>\n") {
			t.Fatal("LOCAL invented a remote outgoing fact instead of using original recipient events")
		}
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
		t.Fatal("fresh provider wire still exposes a retired peer-message schema, index or instruction")
	}
}
