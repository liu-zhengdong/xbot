//go:build smoke

package smoke

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Actual main/research readonly classification for the links runtime namespace:
// search --self and offline help pass through the bound source interpreter,
// while load --self mutates the running kernel's loaded state and is rejected.
func TestCLILinksRuntimeReadOnly(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const worker = "LinksGateWorker"
	srv.CreateBot(worker, "Actual links readonly and runtime-state-load acceptance.")
	var primary struct{ Name string }
	primaryRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(primaryRaw, &primary); err != nil || primary.Name == worker {
		t.Fatal("links readonly source must be actually nonprimary")
	}
	const mainCue, research = "LINKS_GATE_MAIN", "LINKS_GATE_RESEARCH"
	search := `"$XBOT_BIN" links search --self --json`
	workspace := filepath.Join(srv.Root, "work", worker)
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	injection := filepath.Join(workspace, "links-injection-must-not-run")
	model.add(research,
		mcpRuntimeTool("sh", map[string]string{"description": "Search the actual catalog in research", "command": search}),
		mcpRuntimeTool("sh", map[string]string{"description": "Read links help in research", "command": `"$XBOT_BIN" links --help`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Read links search help in research", "command": `"$XBOT_BIN" links search --help`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject links load from research", "command": `"$XBOT_BIN" links load --self tools --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject shell operators in a claimed readonly search", "command": search + `; printf injected > ` + shellQuote(injection)}),
	)
	model.add(mainCue,
		mcpRuntimeTool("dispatch", map[string]string{"type": "research", "task": research}),
	)
	cliSkillSend(t, srv, worker, mainCue)
	var got []string
	WaitFor(t, 15*time.Second, "actual links research readonly steps", func() (bool, string) {
		results := model.results(research)
		if len(results) == 5 && results[4] != "" {
			got = results
			return true, ""
		}
		return false, fmt.Sprintf("results=%.300q", results)
	})
	// search --self and both offline helps reach the real interpreter.
	for i, name := range []string{"search", "help", "search_help"} {
		if !strings.HasPrefix(got[i], "exit 0\n") {
			t.Fatalf("research links %s was rejected: %.900q", name, got[i])
		}
	}
	var reply cliLinksReply
	if err := json.NewDecoder(strings.NewReader(commandOutput(got[0]))).Decode(&reply); err != nil || reply.Error != "" || !strings.Contains(reply.Text, "browser") {
		t.Fatalf("research search did not return the actual catalog: %.900q", got[0])
	}
	// load --self changes k.loaded, a runtime effect, so it stays rejected; the
	// pseudo-search operator never reaches an interpreter either.
	if strings.HasPrefix(got[3], "exit 0\n") || strings.HasPrefix(got[4], "exit 0\n") {
		t.Fatal("research accepted a runtime-state links load or a pseudo-read operator")
	}
	if _, err := os.Stat(injection); !os.IsNotExist(err) {
		t.Fatal("rejected pseudo-read executed its shell operator")
	}
	model.mu.Lock()
	mainSeen := model.scripts[mainCue].callers[callerForeground]
	researchSeen := model.scripts[research].callers[callerSubagent]
	model.mu.Unlock()
	if !mainSeen || !researchSeen {
		t.Fatal("readonly checks did not reach actual main and research provider requests")
	}
}
