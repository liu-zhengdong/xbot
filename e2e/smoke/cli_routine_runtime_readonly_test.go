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
	"strings"
	"sync"
	"testing"
	"time"
)

// Observe actual fresh main/research/dev requests. Legacy provider construction
// remains implementation unit evidence, without another synthetic serve entry.
func TestCLIRoutineRuntimeReadOnlyAndSchemas(t *testing.T) {
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
			for _, name := range []string{"routine_create", "routine_list", "routine_update", "routine_delete"} {
				retired = retired || bytes.Contains(raw, []byte(name))
			}
			mu.Unlock()
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		model.handle(w, r)
	}))
	t.Cleanup(model.server.Close)
	srv := Start(t, &Stub{srv: model.server})
	const worker, id = "RoutineGateWorker", "runtime-gate-entry"
	srv.CreateBot(worker, "Independent actual routine readonly and retired-door acceptance.")
	srv.CreateBot("RoutineGatePeer", "Existing non-routine tool door remains useful.")
	const mainCue, research, dev = "CLI_ROUTINE_GATE_MAIN", "CLI_ROUTINE_GATE_RESEARCH", "CLI_ROUTINE_GATE_DEV"
	createJSON, _ := json.Marshal(map[string]any{"id": id, "name": "Gate entry", "prompt": "ROUTINE_READONLY_PROMPT", "cron": time.Now().Add(24 * time.Hour).Format("4 15 2 1 *")})
	list := `"$XBOT_BIN" routine list --self --json`
	workspace := filepath.Join(srv.Root, "work", worker)
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "native-read-proof.txt"), []byte("OTHER_ROUTINE_NATIVE_READ"), 0600); err != nil {
		t.Fatal(err)
	}
	injection := filepath.Join(workspace, "routine-injection-must-not-run")
	model.add(research,
		mcpRuntimeTool("sh", map[string]string{"description": "List actual routines in research", "command": list}),
		mcpRuntimeTool("sh", map[string]string{"description": "Read routine help in research", "command": `"$XBOT_BIN" routine list --help`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject routine creation from research", "command": `"$XBOT_BIN" routine create --self --args '{"id":"research-must-not-write","prompt":"denied","cron":"0 9 * * *"}' --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject routine update from research", "command": `"$XBOT_BIN" routine update ` + id + ` --self --args '{"name":"Must not update"}' --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject routine deletion from research", "command": `"$XBOT_BIN" routine delete ` + id + ` --self --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject shell operators in a claimed routine list", "command": list + `; printf injected > ` + shellQuote(injection)}),
	)
	model.add(dev,
		mcpRuntimeTool("sh", map[string]string{"description": "List routines from the actual dev source", "command": list}),
		mcpRuntimeTool("read_file", map[string]string{"path": "native-read-proof.txt"}),
	)
	model.add(mainCue,
		mcpRuntimeTool("sh", map[string]string{"description": "Prepare an actual mounted routine for readonly consumers", "command": `"$XBOT_BIN" routine create --self --args ` + shellQuote(string(createJSON)) + ` --json`}),
		mcpRuntimeTool("tools", map[string]any{"action": "call", "id": "list_bots", "args": map[string]any{}}),
		mcpRuntimeTool("dispatch", map[string]string{"type": "research", "task": research}),
		mcpRuntimeTool("dispatch", map[string]string{"type": "dev", "task": dev}),
	)
	cliSkillSend(t, srv, worker, mainCue)
	for _, task := range []string{research, dev} {
		WaitFor(t, 12*time.Second, "actual routine child steps", func() (bool, string) {
			results := model.results(task)
			return results[len(results)-1] != "", "child did not finish its steps"
		})
	}
	mainResults := model.results(mainCue)
	var write cliRoutineWriteReply
	cliRoutineDecode(t, mainResults[0], &write)
	if !write.Applied || write.ID != id || write.Stage != "complete" || write.Error != "" {
		t.Fatal("actual gate fixture create failed")
	}
	if strings.HasPrefix(mainResults[1], "error:") || !strings.Contains(mainResults[1], "RoutineGatePeer") {
		t.Fatal("the other Tools gate stopped executing actual list_bots")
	}
	r := model.results(research)
	var listed cliRoutineListReply
	cliRoutineDecode(t, r[0], &listed)
	if listed.Count != 1 || listed.Routines == nil || len(*listed.Routines) != 1 || (*listed.Routines)[0].ID != id {
		t.Fatal("research did not read the actual routine")
	}
	if !strings.HasPrefix(r[1], "exit 0\n") || !strings.Contains(strings.ToLower(r[1]), "routine") {
		t.Fatal("research routine help was not allowed")
	}
	for _, rejected := range r[2:] {
		if strings.HasPrefix(rejected, "exit 0\n") || strings.TrimSpace(rejected) == "" {
			t.Fatal("research accepted a routine write or pseudo-read operator")
		}
	}
	if _, err := os.Stat(filepath.Join(srv.Root, "bots", worker, "routines", "research-must-not-write.json")); !os.IsNotExist(err) {
		t.Fatal("rejected research create changed the actual routines directory")
	}
	if _, err := os.Stat(injection); !os.IsNotExist(err) {
		t.Fatal("rejected routine pseudo-read executed its shell operator")
	}
	raw, err := os.ReadFile(filepath.Join(srv.Root, "bots", worker, "routines", id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved cliRoutineView
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Name != "Gate entry" || saved.Prompt != "ROUTINE_READONLY_PROMPT" || !saved.Enabled {
		t.Fatal("rejected research mutations changed or removed the original definition")
	}
	d := model.results(dev)
	cliRoutineDecode(t, d[0], &listed)
	if listed.Count != 1 || listed.Routines == nil || len(*listed.Routines) != 1 || (*listed.Routines)[0].ID != id || !strings.Contains(d[1], "OTHER_ROUTINE_NATIVE_READ") {
		t.Fatal("actual dev source list or native file tool regressed")
	}
	model.mu.Lock()
	mainSeen := model.scripts[mainCue].callers[callerForeground]
	researchSeen := model.scripts[research].callers[callerSubagent]
	devSeen := model.scripts[dev].callers[callerSubagent]
	model.mu.Unlock()
	if !mainSeen || !researchSeen || !devSeen {
		t.Fatal("retired-door checks did not reach actual main and continuable provider requests")
	}
	mu.Lock()
	old := retired
	mu.Unlock()
	if old {
		t.Fatal("actual provider request still exposes an old routine schema/index/instruction")
	}
}
