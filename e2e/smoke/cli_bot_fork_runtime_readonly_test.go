//go:build smoke

package smoke

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"xbot/internal/memory"
)

func TestCLIBotForkRuntimeReadOnlyAndSchemas(t *testing.T) {
	cliSkillIsolation(t)
	const handoff = "REAL_ARCHIVE_FORK_GATE_HANDOFF: preserve the known project decision."
	model := newCLIBotForkModel(t, handoff)
	srv := Start(t, &Stub{srv: model.server})
	const source, mainChild, devChild = "ForkGateSource", "ForkGateMainChild", "ForkGateDevChild"
	srv.CreateBot(source, "Actual nonprimary source with project facts for fork readonly acceptance.")
	primaryRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var primary struct{ Name string }
	if err := json.Unmarshal(primaryRaw, &primary); err != nil || primary.Name == "" || primary.Name == source {
		t.Fatal("fork readonly fixture must use an actual nonprimary source")
	}
	// This is ordinary stored Bot fact data in the original memory.md format,
	// before the first source wake. No Memory service or Compactor is replaced.
	memoryPath := filepath.Join(srv.Root, "bots", source, "memory", "memory.md")
	if err := os.MkdirAll(filepath.Dir(memoryPath), 0700); err != nil {
		t.Fatal(err)
	}
	memoryRaw, err := os.ReadFile(memoryPath)
	if os.IsNotExist(err) {
		memoryRaw = []byte("# memory\n\n")
	} else if err != nil {
		t.Fatal(err)
	}
	fact := "- (" + time.Now().Format("2006-01-02") + ") FORK_GATE_FACT_SEEDED: retain the agreed project decision.\n"
	if err := os.WriteFile(memoryPath, append(memoryRaw, []byte(fact)...), 0600); err != nil {
		t.Fatal(err)
	}
	const warm = "FORK_GATE_WARM_A13"
	model.base.add(warm, mcpRuntimeTool("sh", map[string]string{"description": "Warm the original source with nonempty project history and Bot facts", "command": `printf 'FORK_GATE_REAL_HISTORY_READY\n'`}))
	cliSkillSend(t, srv, source, warm)
	if !strings.HasPrefix(model.base.results(warm)[0], "exit 0\n") {
		t.Fatal("actual source did not complete its nonempty warm wake")
	}
	workspace := filepath.Join(srv.Root, "work", source)
	if err := os.WriteFile(filepath.Join(workspace, "fork-native-read-proof.txt"), []byte("FORK_NATIVE_FILE_TOOL_PRESERVED"), 0600); err != nil {
		t.Fatal(err)
	}
	type configView struct {
		Name, Provider, Model string
		Owners, Links         []string
		ContextLimit          int `json:"context_limit"`
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
	if cfg := readConfig(source); len(cfg.Owners) == 0 || cfg.Owners[0] == "" {
		t.Fatal("actual API source lacks its real nonempty owners")
	}
	checkFork := func(result, child, role string) {
		t.Helper()
		var reply cliBotForkReply
		cliBotDecode(t, result, &reply)
		confirmed := slices.Clone(reply.Confirmed)
		slices.Sort(confirmed)
		want := []string{"binding", "child_note", "handoff", "owners", "rules", "source_note"}
		if reply.Name != child || !reply.Created || reply.CreateStage != "" || !slices.Equal(confirmed, want) || reply.HandoffTotal <= 0 || reply.HandoffParts != reply.HandoffTotal || reply.UncertainMemory != nil || reply.GroupID != "" || reply.Stage != "complete" || reply.Error != "" || len(commandOutput(result)) > 32<<10 {
			t.Fatal("actual fork lost its confirmed closed-set progress and complete short receipt")
		}
		parent, got := readConfig(source), readConfig(child)
		if got.Name != child || !slices.Equal(got.Owners, parent.Owners) || !slices.Equal(got.Links, parent.Links) || got.Provider != parent.Provider || got.Model != parent.Model || got.ContextLimit != parent.ContextLimit {
			t.Fatal("actual fork child lost its source owner/link/binding inheritance")
		}
		persona, err := os.ReadFile(filepath.Join(srv.Root, "bots", child, "persona.md"))
		if err != nil || !bytes.Equal(persona, []byte(role+"\n")) {
			t.Fatal("actual fork child did not save its complete role persona")
		}
		childMemory, err := os.ReadFile(filepath.Join(srv.Root, "bots", child, "memory", "memory.md"))
		if err != nil || !bytes.Contains(childMemory, []byte(handoff)) || !bytes.Contains(childMemory, []byte("peer-message")) || !bytes.Contains(childMemory, []byte(source)) {
			t.Fatal("actual child memory lost the real LLM handoff or source recognition fact")
		}
		// Keys/WrittenBy are original index metadata, not dated Markdown text.
		for _, name := range []string{source, child} {
			facts, err := memory.Open(srv.Root).Recall(context.Background(), memory.Query{Scope: memory.ScopeBot, BotName: name, Tier: memory.TierProfile})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, fact := range facts {
				want := "fork-source"
				peer := source
				if name == source {
					want = "fork-" + child
					peer = child
				}
				if fact.Key == want && fact.WrittenBy == source && strings.Contains(fact.Text, peer) && strings.Contains(fact.Text, "peer-message") {
					found = true
				}
			}
			if !found {
				t.Fatal("actual indexed Bot facts lost their acknowledged recognition key/writer/body")
			}
		}
	}
	archiveCount := func() int {
		model.mu.Lock()
		defer model.mu.Unlock()
		return len(model.archive)
	}
	const mainCue, research, dev = "FORK_GATE_MAIN_B24", "FORK_GATE_RESEARCH_C35", "FORK_GATE_DEV_D46"
	const mainRole, devRole = "Review the known project decision.", "Check the project implementation details."
	model.base.add(mainCue,
		mcpRuntimeTool("sh", map[string]string{"description": "Fork a real child from the nonprimary source using the original model path", "command": `"$XBOT_BIN" bot-fork ` + mainChild + ` --self --role ` + shellQuote(mainRole) + ` --json`}),
		mcpRuntimeTool("tools", map[string]string{"action": "describe", "id": "read_file"}),
	)
	cliSkillSend(t, srv, source, mainCue)
	mainResults := model.base.results(mainCue)
	checkFork(mainResults[0], mainChild, mainRole)
	if archiveCount() != 1 {
		t.Fatal("first nonempty fork did not make exactly one actual archive LLM call")
	}
	if strings.HasPrefix(mainResults[1], "error:") || !strings.Contains(mainResults[1], "read_file") {
		t.Fatal("the preserved Tools describe lost native file reading")
	}

	type fileSnapshot struct {
		Exists bool
		Body   []byte
	}
	snapshot := func() map[string]fileSnapshot {
		t.Helper()
		files := map[string]fileSnapshot{}
		botsRoot := filepath.Join(srv.Root, "bots")
		entries, err := os.ReadDir(botsRoot)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			names = append(names, entry.Name())
			path := filepath.Join(botsRoot, entry.Name(), "config.json")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			files[path] = fileSnapshot{Exists: true, Body: raw}
		}
		namesRaw, err := json.Marshal(names)
		if err != nil {
			t.Fatal(err)
		}
		files[botsRoot] = fileSnapshot{Exists: true, Body: namesRaw}
		// Compare forbidden fork writes, not normal dispatch notifications,
		// source transcript/usage, or already-warmed card initialization.
		for _, root := range []string{filepath.Join(botsRoot, source, "memory"), filepath.Join(botsRoot, mainChild, "memory"), filepath.Join(srv.Root, "groups")} {
			if _, err := os.Stat(root); os.IsNotExist(err) {
				files[root] = fileSnapshot{}
				continue
			} else if err != nil {
				t.Fatal(err)
			}
			files[root] = fileSnapshot{Exists: true}
			if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
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
		}
		return files
	}
	beforeResearch, callsBeforeResearch := snapshot(), archiveCount()
	injection := filepath.Join(workspace, "fork-operator-must-not-run")
	model.base.add(research,
		mcpRuntimeTool("sh", map[string]string{"description": "Read bot-fork help in research", "command": `"$XBOT_BIN" bot-fork --help`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject ordinary fork writes before the interpreter", "command": `"$XBOT_BIN" bot-fork ForkResearchDenied --self --role 'Must not create' --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject literal help role as a write rather than granting help", "command": `"$XBOT_BIN" bot-fork ForkLiteralRoleDenied --self --role --help --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject literal help name as a write rather than granting help", "command": `"$XBOT_BIN" bot-fork --self --role 'Must not create' --json -- --help`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject missing-self fork writes before the interpreter", "command": `"$XBOT_BIN" bot-fork ForkMissingSelfDenied --role 'Must not create' --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject shell operators attached to fork help", "command": `"$XBOT_BIN" bot-fork --help; printf injected > ` + shellQuote(injection)}),
	)
	const researchDispatch = "FORK_DISPATCH_RESEARCH_E57"
	model.base.add(researchDispatch, mcpRuntimeTool("dispatch", map[string]string{"type": "research", "task": research}))
	cliSkillSend(t, srv, source, researchDispatch)
	WaitFor(t, 12*time.Second, "actual bot-fork research steps", func() (bool, string) {
		results := model.base.results(research)
		return results[len(results)-1] != "", "research steps not completed"
	})
	r := model.base.results(research)
	if !strings.HasPrefix(r[0], "exit 0\n") || strings.TrimSpace(commandOutput(r[0])) == "" || len(commandOutput(r[0])) > 32<<10 {
		t.Fatal("research did not allow bounded actual fork help")
	}
	for _, rejected := range r[1:] {
		line, _, _ := strings.Cut(rejected, "\n")
		if exitLine.MatchString(line) || strings.TrimSpace(rejected) == "" || len(rejected) > 32<<10 {
			t.Fatal("research fork, literal-help write or pseudo-help reached the interpreter")
		}
	}
	if archiveCount() != callsBeforeResearch || !reflect.DeepEqual(beforeResearch, snapshot()) {
		t.Fatal("rejected research commands invoked the archive model or changed actual fork state")
	}
	if _, err := os.Stat(injection); !os.IsNotExist(err) {
		t.Fatal("rejected pseudo-help executed its shell operator")
	}

	// Dev starts after the refusal comparison and uses its own real native Job
	// context. No forged Call-ID, latest-turn context or child service is supplied.
	model.base.add(dev,
		mcpRuntimeTool("sh", map[string]string{"description": "Fork another actual child through the same-source dev Shell Job", "command": `"$XBOT_BIN" bot-fork ` + devChild + ` --self --role ` + shellQuote(devRole) + ` --json`}),
		mcpRuntimeTool("read_file", map[string]string{"path": "fork-native-read-proof.txt"}),
	)
	const devDispatch = "FORK_DISPATCH_DEV_F68"
	model.base.add(devDispatch, mcpRuntimeTool("dispatch", map[string]string{"type": "dev", "task": dev}))
	cliSkillSend(t, srv, source, devDispatch)
	WaitFor(t, 12*time.Second, "actual bot-fork dev steps", func() (bool, string) {
		results := model.base.results(dev)
		return results[len(results)-1] != "", "dev steps not completed"
	})
	d := model.base.results(dev)
	checkFork(d[0], devChild, devRole)
	if !strings.Contains(d[1], "FORK_NATIVE_FILE_TOOL_PRESERVED") {
		t.Fatal("actual dev source lost its preserved native file tool")
	}
	model.base.mu.Lock()
	mainSeen := model.base.scripts[mainCue].callers[callerForeground]
	researchSeen := model.base.scripts[research].callers[callerSubagent]
	devSeen := model.base.scripts[dev].callers[callerSubagent]
	model.base.mu.Unlock()
	if !mainSeen || !researchSeen || !devSeen {
		t.Fatal("retirement checks did not reach fresh actual main and continuable requests")
	}
	model.mu.Lock()
	archives := append([]cliBotForkArchiveRequest(nil), model.archive...)
	retired := false
	for _, raw := range model.raw {
		var request stubRequest
		if json.Unmarshal(raw, &request) == nil {
			caller, _ := classify(request)
			if caller != callerCompaction && caller != callerExtract && bytes.Contains(raw, []byte("fork_bot")) {
				retired = true
			}
		}
	}
	model.mu.Unlock()
	if len(archives) != 2 {
		t.Fatalf("two allowed forks made %d actual archive LLM calls", len(archives))
	}
	for i, request := range archives {
		role := mainRole
		if i == 1 {
			role = devRole
		}
		if len(request.Tools) != 0 || !strings.Contains(joined(request.stubRequest), role) || !strings.Contains(joined(request.stubRequest), "FORK_GATE_FACT_SEEDED") {
			t.Fatal("actual archive call did not preserve noTools Chat, role focus and real source Bot facts")
		}
	}
	if retired {
		t.Fatal("fresh provider requests still expose the retired fork schema, index or instruction")
	}
}
