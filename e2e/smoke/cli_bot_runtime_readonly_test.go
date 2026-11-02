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

// Observe actual fresh main and continuable research/dev provider requests.
// Legacy request construction remains implementation unit coverage; no second
// serve entry, synthetic policy or approval context is introduced here.
func TestCLIBotRuntimeReadOnlyAndSchemas(t *testing.T) {
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
			for _, name := range []string{"list_bots", "bot_status", "create_bot", "delete_bot"} {
				retired = retired || bytes.Contains(raw, []byte(name))
			}
			mu.Unlock()
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		model.handle(w, r)
	}))
	t.Cleanup(model.server.Close)
	srv := Start(t, &Stub{srv: model.server})
	const peer = "BotGatePeer"
	srv.CreateBot(peer, "An actual peer for preserved pinning and readonly discovery.")
	primaryRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var primary struct{ Name string }
	if err := json.Unmarshal(primaryRaw, &primary); err != nil {
		t.Fatal(err)
	}
	if primary.Name == "" || primary.Name == peer {
		t.Fatal("could not resolve this private instance's actual Primary source")
	}
	type configView struct {
		Name, Persona string
		Owners        []string
		Pinned        bool
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
	if !shared && len(beforePeer.Owners) > 0 {
		// Bind a potentially ownerless real seed before its first source wake;
		// preserve all existing owners, Links and unknown persisted fields.
		path := filepath.Join(srv.Root, "bots", primary.Name, "config.json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var cfg map[string]json.RawMessage
		if err := json.Unmarshal(raw, &cfg); err != nil {
			t.Fatal(err)
		}
		owners := slices.Clone(source.Owners)
		for _, owner := range beforePeer.Owners {
			if owner != "" && !slices.Contains(owners, owner) {
				owners = append(owners, owner)
			}
		}
		cfg["owners"], _ = json.Marshal(owners)
		bound, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(bound, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
		source = readConfig(primary.Name)
		for _, owner := range source.Owners {
			shared = shared || owner != "" && slices.Contains(beforePeer.Owners, owner)
		}
	}
	if !shared || beforePeer.Pinned {
		t.Fatal("actual Primary and fresh unpinned peer lack the required authenticated ownership fixture")
	}
	workspace := filepath.Join(srv.Root, "work", primary.Name)
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "bot-native-read-proof.txt"), []byte("BOT_OTHER_NATIVE_TOOL_PRESERVED"), 0600); err != nil {
		t.Fatal(err)
	}
	const mainCue, research, dev = "CLI_BOT_GATE_MAIN", "CLI_BOT_GATE_RESEARCH", "CLI_BOT_GATE_DEV"
	const deniedRuntime, deniedOffline = "BotResearchMustNotCreate", "BotOfflineMustNotCreate"
	list := `"$XBOT_BIN" list --self --json`
	info := `"$XBOT_BIN" info ` + shellQuote(primary.Name) + ` --self --json`
	injection := filepath.Join(workspace, "bot-operator-must-not-run")
	model.add(research,
		mcpRuntimeTool("sh", map[string]string{"description": "List visible Bots in research", "command": list}),
		mcpRuntimeTool("sh", map[string]string{"description": "Read the actual Primary host status in research", "command": info}),
		mcpRuntimeTool("sh", map[string]string{"description": "Read offline-safe Bot CLI help in research", "command": `"$XBOT_BIN" list --help`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject runtime Bot creation from research", "command": `"$XBOT_BIN" new ` + deniedRuntime + ` --self --persona 'Must not create from research' --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject runtime Bot deletion from research", "command": `"$XBOT_BIN" delete ` + peer + ` --self --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject the old offline creation path from research", "command": `"$XBOT_BIN" new ` + deniedOffline + ` --persona 'Offline creation must not run'`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject shell operators in a claimed readonly list", "command": list + `; printf injected > ` + shellQuote(injection)}),
	)
	model.add(dev,
		mcpRuntimeTool("sh", map[string]string{"description": "Read host Bot status through an actual dev source", "command": info}),
		mcpRuntimeTool("read_file", map[string]string{"path": "bot-native-read-proof.txt"}),
	)
	model.add(mainCue,
		mcpRuntimeTool("sh", map[string]string{"description": "Pin the actual peer through the migrated source CLI", "command": `"$XBOT_BIN" pin ` + peer + ` --self --json`}),
		mcpRuntimeTool("tools", map[string]string{"action": "describe", "id": "read_file"}),
		mcpRuntimeTool("dispatch", map[string]string{"type": "research", "task": research}),
		mcpRuntimeTool("dispatch", map[string]string{"type": "dev", "task": dev}),
	)
	cliSkillSend(t, srv, primary.Name, mainCue)
	for _, task := range []string{research, dev} {
		WaitFor(t, 12*time.Second, "actual Bot child steps", func() (bool, string) {
			results := model.results(task)
			return results[len(results)-1] != "", "child steps not completed"
		})
	}
	mainResults := model.results(mainCue)
	// ADR0053 retires native pin/primary; retain the prior actual pinning effect
	// via its current CLI and keep Tools describe on still-live file reading.
	var pinned struct {
		Name            string
		Pinned, Applied bool
		Stage, Error    string
	}
	cliBotDecode(t, mainResults[0], &pinned)
	if pinned.Name != peer || !pinned.Pinned || !pinned.Applied || pinned.Stage != "complete" || pinned.Error != "" {
		t.Fatal("migrated source CLI did not report the actual saved peer pin")
	}
	if strings.HasPrefix(mainResults[1], "error:") || !strings.Contains(mainResults[1], "read_file") {
		t.Fatal("the preserved Tools describe lost unaffected file reading")
	}
	afterPeer := readConfig(peer)
	if !afterPeer.Pinned || afterPeer.Name != beforePeer.Name || afterPeer.Persona != beforePeer.Persona || !slices.Equal(afterPeer.Owners, beforePeer.Owners) {
		t.Fatal("actual pinning did not persist or rejected research deletion changed the real peer")
	}
	r := model.results(research)
	var listed cliBotListReply
	cliBotDecode(t, r[0], &listed)
	if listed.Local == nil || listed.Remote == nil || listed.Count != len(*listed.Local)+len(*listed.Remote) || listed.File != "" || listed.Error != "" {
		t.Fatal("research List did not return complete actual discovery arrays")
	}
	peerSeen := false
	for _, item := range *listed.Local {
		peerSeen = peerSeen || item.Name == peer
	}
	if !peerSeen {
		t.Fatal("research List lost the actual same-owner peer")
	}
	checkInfo := func(result string) {
		t.Helper()
		var reply cliBotInfoReply
		cliBotDecode(t, result, &reply)
		if reply.Bots == nil || len(*reply.Bots) != 1 || reply.Count != 1 || (*reply.Bots)[0].Bot != primary.Name || (*reply.Bots)[0].State == "" || reply.File != "" || reply.Error != "" {
			t.Fatal("actual child source Info lost the requested Primary host status")
		}
	}
	checkInfo(r[1])
	if !strings.HasPrefix(r[2], "exit 0\n") || strings.TrimSpace(commandOutput(r[2])) == "" {
		t.Fatal("research did not allow real Bot CLI help")
	}
	for _, rejected := range r[3:] {
		if strings.HasPrefix(rejected, "exit 0\n") || strings.TrimSpace(rejected) == "" || len(rejected) > 64<<10 {
			t.Fatal("research allowed a Bot write, offline command or shell operator")
		}
	}
	for _, name := range []string{deniedRuntime, deniedOffline} {
		if _, err := os.Stat(filepath.Join(srv.Root, "bots", name)); !os.IsNotExist(err) {
			t.Fatal("rejected research creation left an actual Bot directory")
		}
	}
	if _, err := os.Stat(injection); !os.IsNotExist(err) {
		t.Fatal("rejected pseudo-read executed its shell operator")
	}
	d := model.results(dev)
	checkInfo(d[0])
	if !strings.Contains(d[1], "BOT_OTHER_NATIVE_TOOL_PRESERVED") {
		t.Fatal("the actual dev source lost its preserved native file tool")
	}
	model.mu.Lock()
	mainSeen := model.scripts[mainCue].callers[callerForeground]
	researchSeen := model.scripts[research].callers[callerSubagent]
	devSeen := model.scripts[dev].callers[callerSubagent]
	model.mu.Unlock()
	if !mainSeen || !researchSeen || !devSeen {
		t.Fatal("retirement checks did not reach actual fresh main and continuable provider requests")
	}
	mu.Lock()
	old := retired
	mu.Unlock()
	if old {
		t.Fatal("actual provider requests still expose a retired Bot schema, index or instruction")
	}
}
