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

// One actual nonprimary source exercises main and continuable research/dev.
// Only the upstream provider is scripted; no approval context or source identity
// is manufactured, and the retired action is never called by these scripts.
func TestCLIPersonaRuntimeReadOnlyAndSchemas(t *testing.T) {
	cliSkillIsolation(t)
	model := newCLIPersonaModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const source = "PersonaGateWorker"
	srv.CreateBot(source, "Original nonprimary persona readonly acceptance duties.")
	primaryRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var primary struct{ Name string }
	if err := json.Unmarshal(primaryRaw, &primary); err != nil || primary.Name == "" || primary.Name == source {
		t.Fatal("persona readonly fixture must use an actual nonprimary source")
	}
	const warm = "CLI_PERSONA_GATE_WARM"
	model.base.add(warm, mcpRuntimeTool("sh", map[string]string{"description": "Warm the actual source and its original seeded persona card", "command": `printf 'persona-readonly-source-ready\n'`}))
	cliSkillSend(t, srv, source, warm)
	if !strings.HasPrefix(model.base.results(warm)[0], "exit 0\n") {
		t.Fatal("actual source failed its initial native warm wake")
	}
	workspace := filepath.Join(srv.Root, "work", source)
	if err := os.WriteFile(filepath.Join(workspace, "research-denied-duty.txt"), []byte("RESEARCH_MUST_NOT_REPLACE_DUTIES"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "persona-native-read-proof.txt"), []byte("PERSONA_NATIVE_FILE_TOOL_PRESERVED"), 0600); err != nil {
		t.Fatal(err)
	}
	personaPath := filepath.Join(srv.Root, "bots", source, "persona.md")
	checkWrite := func(result, duty string) {
		t.Helper()
		var reply cliPersonaReply
		cliBotDecode(t, result, &reply)
		if reply.Name != source || !reply.Written || reply.Effective != "next_wake" || reply.Error != "" || len(commandOutput(result)) > 32<<10 {
			t.Fatal("actual nonprimary source setter lost its exact written/next-wake receipt")
		}
		stored, err := os.ReadFile(personaPath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(stored, []byte(duty+"\n")) {
			t.Fatal("actual nonprimary source did not save its complete replacement duties")
		}
	}
	const mainCue, research, dev = "CLI_PERSONA_GATE_MAIN", "CLI_PERSONA_GATE_RESEARCH", "CLI_PERSONA_GATE_DEV"
	const mainDuty, devDuty = "MAIN_PERSONA_READONLY_DUTY", "DEV_PERSONA_READONLY_DUTY"
	model.base.add(mainCue,
		mcpRuntimeTool("sh", map[string]string{"description": "Replace this nonprimary source's duties with a tiny text input", "command": `"$XBOT_BIN" persona set --self --text ` + shellQuote(mainDuty) + ` --json`}),
		mcpRuntimeTool("tools", map[string]string{"action": "describe", "id": "recall_history"}),
	)
	cliSkillSend(t, srv, source, mainCue)
	mainResults := model.base.results(mainCue)
	checkWrite(mainResults[0], mainDuty)
	if strings.HasPrefix(mainResults[1], "error:") || !strings.Contains(mainResults[1], "recall_history") {
		t.Fatal("the preserved Tools describe lost native history recall")
	}

	type fileSnapshot struct {
		Exists bool
		Body   []byte
	}
	snapshot := func() map[string]fileSnapshot {
		t.Helper()
		files := map[string]fileSnapshot{}
		for _, path := range []string{personaPath, filepath.Join(srv.Root, "bots", source, "config.json"), filepath.Join(srv.Root, "bots", source, "rules.md")} {
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
		// First-wake PickedCard/EnsureSeeds already ran through the real source.
		// Preserve both library content and its selection/configuration files.
		cards := filepath.Join(srv.Root, "persona-cards")
		if err := filepath.WalkDir(cards, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
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
	injection := filepath.Join(workspace, "persona-operator-must-not-run")
	model.base.add(research,
		mcpRuntimeTool("sh", map[string]string{"description": "Read persona command help in research", "command": `"$XBOT_BIN" persona --help`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Read persona setter help in research", "command": `"$XBOT_BIN" persona set --help`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject persona text replacement before the interpreter", "command": `"$XBOT_BIN" persona set --self --text RESEARCH_TEXT_MUST_NOT_WRITE --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject persona file replacement before opening the real input", "command": `"$XBOT_BIN" persona set --self --file ./research-denied-duty.txt --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject persona stdin replacement before the interpreter", "command": `"$XBOT_BIN" persona set --self --file - --json < ./research-denied-duty.txt`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject missing-self persona writes before the interpreter", "command": `"$XBOT_BIN" persona set --text RESEARCH_NO_SELF_MUST_NOT_WRITE --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject shell operators appended to persona help", "command": `"$XBOT_BIN" persona --help; printf injected > ` + shellQuote(injection)}),
	)
	const researchDispatch = "CLI_PERSONA_GATE_DISPATCH_RESEARCH"
	model.base.add(researchDispatch, mcpRuntimeTool("dispatch", map[string]string{"type": "research", "task": research}))
	cliSkillSend(t, srv, source, researchDispatch)
	WaitFor(t, 12*time.Second, "actual persona research steps", func() (bool, string) {
		results := model.base.results(research)
		return results[len(results)-1] != "", "research steps not completed"
	})
	r := model.base.results(research)
	for _, help := range r[:2] {
		if !strings.HasPrefix(help, "exit 0\n") || strings.TrimSpace(commandOutput(help)) == "" || len(commandOutput(help)) > 32<<10 {
			t.Fatal("research did not allow bounded actual persona help")
		}
	}
	for _, rejected := range r[2:] {
		line, _, _ := strings.Cut(rejected, "\n")
		if exitLine.MatchString(line) || strings.TrimSpace(rejected) == "" || len(rejected) > 32<<10 {
			t.Fatal("research persona write or pseudo-help reached the interpreter instead of being refused")
		}
	}
	if !reflect.DeepEqual(beforeResearch, snapshot()) {
		t.Fatal("rejected research commands changed actual persona/config/rules/card state")
	}
	if _, err := os.Stat(injection); !os.IsNotExist(err) {
		t.Fatal("rejected pseudo-help executed its shell operator")
	}

	// The comparison above precedes dev's legitimate write, so dev cannot mask
	// a research overwrite. Dev uses the same actual nonprimary self source.
	model.base.add(dev,
		mcpRuntimeTool("sh", map[string]string{"description": "Replace duties from an actual nonprimary dev source", "command": `"$XBOT_BIN" persona set --self --text ` + shellQuote(devDuty) + ` --json`}),
		mcpRuntimeTool("read_file", map[string]string{"path": "persona-native-read-proof.txt"}),
	)
	const devDispatch = "CLI_PERSONA_GATE_DISPATCH_DEV"
	model.base.add(devDispatch, mcpRuntimeTool("dispatch", map[string]string{"type": "dev", "task": dev}))
	cliSkillSend(t, srv, source, devDispatch)
	WaitFor(t, 12*time.Second, "actual persona dev steps", func() (bool, string) {
		results := model.base.results(dev)
		return results[len(results)-1] != "", "dev steps not completed"
	})
	d := model.base.results(dev)
	checkWrite(d[0], devDuty)
	if !strings.Contains(d[1], "PERSONA_NATIVE_FILE_TOOL_PRESERVED") {
		t.Fatal("actual dev source lost its preserved native file tool")
	}
	model.base.mu.Lock()
	mainSeen := model.base.scripts[mainCue].callers[callerForeground]
	researchSeen := model.base.scripts[research].callers[callerSubagent]
	devSeen := model.base.scripts[dev].callers[callerSubagent]
	model.base.mu.Unlock()
	if !mainSeen || !researchSeen || !devSeen || len(model.requests(mainCue)) == 0 || len(model.requests(research)) == 0 || len(model.requests(dev)) == 0 {
		t.Fatal("retirement checks did not reach actual fresh main and continuable provider requests")
	}
	model.mu.Lock()
	retired := false
	for _, raw := range model.raw {
		var request stubRequest
		if json.Unmarshal(raw, &request) != nil {
			continue
		}
		caller, _ := classify(request)
		if caller != callerCompaction && caller != callerExtract && bytes.Contains(raw, []byte("bot_propose_persona")) {
			retired = true
		}
	}
	model.mu.Unlock()
	if retired {
		t.Fatal("actual provider requests still expose the retired persona schema, index or instruction")
	}
}
