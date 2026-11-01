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
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// Observe the actual main and continuable research/dev provider requests.
// Legacy construction stays implementation unit coverage. No source ownership,
// policy, approval context or already-captured Resources are manufactured here.
func TestCLIBotControlRuntimeReadOnlyAndSchemas(t *testing.T) {
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
			for _, name := range []string{"pin_bot", "set_primary"} {
				retired = retired || bytes.Contains(raw, []byte(name))
			}
			mu.Unlock()
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		model.handle(w, r)
	}))
	t.Cleanup(model.server.Close)
	srv := Start(t, &Stub{srv: model.server})
	const peer = "ControlGatePeer"
	srv.CreateBot(peer, "Actual same-owner peer for control CLI readonly acceptance.")
	primaryPath := filepath.Join(srv.Root, "primary.json")
	primaryRaw, err := os.ReadFile(primaryPath)
	if err != nil {
		t.Fatal(err)
	}
	var primary struct{ Name string }
	if err := json.Unmarshal(primaryRaw, &primary); err != nil || primary.Name == "" || primary.Name == peer {
		t.Fatal("fixture did not resolve the actual root Primary source")
	}
	type configView struct {
		Owners []string
		Pinned bool
	}
	readConfig := func(name string) configView {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(srv.Root, "bots", name, "config.json"))
		if err != nil {
			t.Fatal(err)
		}
		var cfg configView
		if err := json.Unmarshal(raw, &cfg); err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	source, beforePeer := readConfig(primary.Name), readConfig(peer)
	shared := false
	for _, owner := range source.Owners {
		shared = shared || owner != "" && slices.Contains(beforePeer.Owners, owner)
	}
	if !shared || beforePeer.Pinned {
		t.Fatal("actual Primary and fresh unpinned API peer lack the authenticated owner intersection")
	}
	workspace := filepath.Join(srv.Root, "work", primary.Name)
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "control-native-read-proof.txt"), []byte("CONTROL_NATIVE_FILE_TOOL_PRESERVED"), 0600); err != nil {
		t.Fatal(err)
	}
	pin := `"$XBOT_BIN" pin ` + peer + ` --self --json`
	const mainCue, research, dev = "CLI_CONTROL_GATE_MAIN", "CLI_CONTROL_GATE_RESEARCH", "CLI_CONTROL_GATE_DEV"
	model.add(mainCue,
		mcpRuntimeTool("sh", map[string]string{"description": "Pin an actual peer using the current Primary source CLI", "command": pin}),
		mcpRuntimeTool("tools", map[string]string{"action": "describe", "id": "recall_history"}),
	)
	cliSkillSend(t, srv, primary.Name, mainCue)
	mainResults := model.results(mainCue)
	checkPin := func(result string) {
		t.Helper()
		var reply cliBotControlPinReply
		cliBotDecode(t, result, &reply)
		if reply.Name != peer || !reply.Pinned || !reply.Applied || reply.Stage != "complete" || reply.Error != "" || len(commandOutput(result)) > 32<<10 {
			t.Fatal("source pin did not report the exact typed saved result")
		}
		if !readConfig(peer).Pinned {
			t.Fatal("actual peer config did not persist Pinned=true")
		}
	}
	checkPin(mainResults[0])
	if strings.HasPrefix(mainResults[1], "error:") || !strings.Contains(mainResults[1], "recall_history") {
		t.Fatal("the preserved Tools describe lost the live history action")
	}

	// Finish research and inspect disk before dev can repeat a legitimate pin.
	// Otherwise an incorrectly executed research unpin could be masked by dev.
	paths := []string{primaryPath, filepath.Join(srv.Root, "bots", primary.Name, "config.json"), filepath.Join(srv.Root, "bots", peer, "config.json")}
	before := make(map[string][]byte, len(paths))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = raw
	}
	injection := filepath.Join(workspace, "control-operator-must-not-run")
	model.add(research,
		mcpRuntimeTool("sh", map[string]string{"description": "Read pin CLI help in research", "command": `"$XBOT_BIN" pin --help`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Read primary CLI help in research", "command": `"$XBOT_BIN" primary --help`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject runtime pin from research", "command": pin}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject runtime unpin from research", "command": `"$XBOT_BIN" unpin ` + peer + ` --self --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject runtime primary transfer from research", "command": `"$XBOT_BIN" primary set ` + peer + ` --self --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject the existing offline primary command from research", "command": `"$XBOT_BIN" primary`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject shell operators appended to claimed readonly help", "command": `"$XBOT_BIN" pin --help; printf injected > ` + shellQuote(injection)}),
	)
	const researchDispatch = "CLI_CONTROL_GATE_DISPATCH_RESEARCH"
	model.add(researchDispatch, mcpRuntimeTool("dispatch", map[string]string{"type": "research", "task": research}))
	cliSkillSend(t, srv, primary.Name, researchDispatch)
	WaitFor(t, 12*time.Second, "actual control research steps", func() (bool, string) {
		results := model.results(research)
		return results[len(results)-1] != "", "research steps not completed"
	})
	r := model.results(research)
	for _, help := range r[:2] {
		if !strings.HasPrefix(help, "exit 0\n") || strings.TrimSpace(commandOutput(help)) == "" || len(commandOutput(help)) > 32<<10 {
			t.Fatal("research did not allow bounded offline-safe control help")
		}
	}
	for _, rejected := range r[2:] {
		if strings.HasPrefix(rejected, "exit 0\n") || strings.TrimSpace(rejected) == "" || len(rejected) > 64<<10 {
			t.Fatal("research allowed a runtime control write, offline command or shell operator")
		}
	}
	for path, want := range before {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("rejected research operation changed %s", filepath.Base(path))
		}
	}
	if _, err := os.Stat(injection); !os.IsNotExist(err) {
		t.Fatal("rejected pseudo-help executed its shell operator")
	}

	model.add(dev,
		mcpRuntimeTool("sh", map[string]string{"description": "Pin the peer from the actual same-Primary dev source", "command": pin}),
		mcpRuntimeTool("read_file", map[string]string{"path": "control-native-read-proof.txt"}),
	)
	const devDispatch = "CLI_CONTROL_GATE_DISPATCH_DEV"
	model.add(devDispatch, mcpRuntimeTool("dispatch", map[string]string{"type": "dev", "task": dev}))
	cliSkillSend(t, srv, primary.Name, devDispatch)
	WaitFor(t, 12*time.Second, "actual control dev steps", func() (bool, string) {
		results := model.results(dev)
		return results[len(results)-1] != "", "dev steps not completed"
	})
	d := model.results(dev)
	checkPin(d[0])
	if !strings.Contains(d[1], "CONTROL_NATIVE_FILE_TOOL_PRESERVED") {
		t.Fatal("actual dev source lost its preserved native file tool")
	}
	model.mu.Lock()
	mainSeen := model.scripts[mainCue].callers[callerForeground]
	researchSeen := model.scripts[research].callers[callerSubagent]
	devSeen := model.scripts[dev].callers[callerSubagent]
	model.mu.Unlock()
	if !mainSeen || !researchSeen || !devSeen {
		t.Fatal("retirement checks did not reach fresh actual main and continuable provider requests")
	}
	mu.Lock()
	old := retired
	mu.Unlock()
	if old {
		t.Fatal("actual provider requests still expose a retired control schema, index or instruction")
	}
}
