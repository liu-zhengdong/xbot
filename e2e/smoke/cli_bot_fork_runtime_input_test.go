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

func TestCLIBotForkRuntimeRoleFileAndStdin(t *testing.T) {
	cliSkillIsolation(t)
	const handoff = "ROLE_INPUT_ARCHIVE_HANDOFF: retain the accepted project decision and review its implementation carefully."
	model := newCLIBotForkModel(t, handoff)
	srv := Start(t, &Stub{srv: model.server})
	const source, fileChild, stdinChild = "ForkInputSource", "ForkRoleFileChild", "ForkRoleStdinChild"
	srv.CreateBot(source, "Actual nonprimary source with persistent project context for complete role inputs.")
	primaryRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var primary struct{ Name string }
	if err := json.Unmarshal(primaryRaw, &primary); err != nil || primary.Name == "" || primary.Name == source {
		t.Fatal("role input acceptance must use the actual nonprimary source")
	}
	type configView struct {
		Name, Provider, Model, Role string
		Owners, Links               []string
		ContextLimit                int `json:"context_limit"`
		BackgroundMaxTime           int `json:"background_max_time"`
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
	sourceConfig := readConfig(source)
	if len(sourceConfig.Owners) == 0 || sourceConfig.Owners[0] == "" {
		t.Fatal("actual API source has no real owner")
	}
	// Public persistence APIs create valid facts; neither the Kernel's Memory
	// implementation nor its real Compactor is replaced by the fixture.
	store := memory.Open(srv.Root)
	const botFact = "FORK_ROLE_INPUT_BOT_FACT: retain the accepted project decision."
	const personFact = "SHARED_PERSON_FACT_MUST_NOT_BE_COPIED_TO_CHILD"
	for _, fact := range []memory.Fact{
		{Scope: memory.ScopeBot, BotName: source, Tier: memory.TierProfile, Key: "role-input-decision", Kind: memory.KindWork, WrittenBy: source, Text: botFact, Time: time.Now().UTC()},
		{Scope: memory.ScopePerson, PersonID: sourceConfig.Owners[0], Tier: memory.TierProfile, Key: "shared-input-owner", Kind: memory.KindAttribute, WrittenBy: source, Text: personFact, Time: time.Now().UTC()},
	} {
		if err := store.Remember(context.Background(), fact); err != nil {
			t.Fatal(err)
		}
	}
	run := func(cue, command string) string {
		t.Helper()
		model.base.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Use complete role input through the actual source native Shell", "command": command}))
		cliSkillSend(t, srv, source, cue)
		results := model.base.results(cue)
		if len(results) != 1 || len(commandOutput(results[0])) > 32<<10 {
			t.Fatal("actual role input call did not return one bounded native Shell result")
		}
		return results[0]
	}
	if !strings.HasPrefix(run("FORK_INPUT_WARM_A11", `printf 'ROLE_INPUT_ACTUAL_HISTORY\n'`), "exit 0\n") {
		t.Fatal("actual warm source history was not persisted")
	}
	// Reload this private process only after the actual history and valid Bot
	// fact exist; both successful forks must use the nonempty original LLM path.
	srv.Restart()

	roleOne := "FILE_ROLE:" + strings.Repeat("职责", 1363)
	roleOne += strings.Repeat("x", 8192-len(roleOne))
	roleTwo := "STDIN_ROLE:" + strings.Repeat("审阅", 1363)
	roleTwo += strings.Repeat("y", 8192-len(roleTwo))
	if len(roleOne) != 8192 || len(roleTwo) != 8192 {
		t.Fatal("both effective UTF-8 roles must be exactly 8192 bytes")
	}
	workspace := filepath.Join(srv.Root, "work", source)
	onePath, twoPath := filepath.Join(workspace, "role-one.txt"), filepath.Join(workspace, "role-two.txt")
	oneInput := []byte(strings.Repeat(" ", 20<<10) + roleOne + "\n\t")
	twoInput := []byte(strings.Repeat(" ", 20<<10) + roleTwo + "\n\t")
	if err := os.WriteFile(onePath, oneInput, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(twoPath, twoInput, 0600); err != nil {
		t.Fatal(err)
	}
	personPath := filepath.Join(srv.Root, "people", sourceConfig.Owners[0], "memory", "memory.md")
	personBefore, err := os.ReadFile(personPath)
	if err != nil || !bytes.Contains(personBefore, []byte(personFact)) {
		t.Fatal("shared person fact did not reach its original physical scope")
	}
	checkChild := func(result, child, role string) {
		t.Helper()
		var reply cliBotForkReply
		cliBotDecode(t, result, &reply)
		confirmed := slices.Clone(reply.Confirmed)
		slices.Sort(confirmed)
		if reply.Name != child || !reply.Created || reply.CreateStage != "" || !slices.Equal(confirmed, []string{"binding", "child_note", "handoff", "owners", "rules", "source_note"}) || reply.HandoffTotal <= 0 || reply.HandoffParts != reply.HandoffTotal || reply.UncertainMemory != nil || reply.GroupID != "" || reply.Stage != "complete" || reply.Error != "" {
			t.Fatal("full role fork did not report all acknowledged setup stages")
		}
		output := commandOutput(result)
		if strings.Contains(output, role) || strings.Contains(output, handoff) || strings.Contains(output, `"file"`) || strings.Contains(output, `"access"`) {
			t.Fatal("short fork ACK exposed full role/handoff or invented a result artifact")
		}
		cfg := readConfig(child)
		if cfg.Name != child || !slices.Equal(cfg.Owners, sourceConfig.Owners) || !slices.Equal(cfg.Links, sourceConfig.Links) || cfg.Provider != sourceConfig.Provider || cfg.Model != sourceConfig.Model || cfg.ContextLimit != sourceConfig.ContextLimit || cfg.Role != "" && cfg.Role != "chat" || cfg.BackgroundMaxTime != 0 {
			t.Fatal("child lost source bindings or normal chat/default 45-minute configuration")
		}
		stored, err := os.ReadFile(filepath.Join(srv.Root, "bots", child, "persona.md"))
		if err != nil || !bytes.Equal(stored, []byte(role+"\n")) {
			t.Fatal("actual child persona clipped the full 8192-byte UTF-8 role")
		}
		facts, err := store.Recall(context.Background(), memory.Query{Scope: memory.ScopeBot, BotName: child, Tier: memory.TierProfile})
		if err != nil {
			t.Fatal(err)
		}
		handoffParts, sourceNote := 0, false
		for _, fact := range facts {
			if fact.Scope != memory.ScopeBot || fact.BotName != child || fact.WrittenBy != source || strings.Contains(fact.Text, personFact) {
				t.Fatal("actual child memory metadata copied a person scope or lost source authorship")
			}
			if fact.Key == "handoff" || strings.HasPrefix(fact.Key, "handoff-") {
				handoffParts++
				if fact.Text != handoff {
					t.Fatal("actual original LLM handoff did not reach child profile memory")
				}
			}
			sourceNote = sourceNote || fact.Key == "fork-source" && strings.Contains(fact.Text, source)
		}
		if handoffParts != reply.HandoffParts || !sourceNote {
			t.Fatal("actual child memory disagrees with acknowledged handoff and source note")
		}
		if _, err := os.Stat(filepath.Join(srv.Root, "bots", child, "people")); !os.IsNotExist(err) {
			t.Fatal("fork copied a private person directory into the child")
		}
	}
	oneResult := run("FORK_INPUT_FILE_B22", `"$XBOT_BIN" bot-fork `+fileChild+` --self --role-file ./role-one.txt --json`)
	checkChild(oneResult, fileChild, roleOne)
	actualOne, err := os.ReadFile(onePath)
	if err != nil || !bytes.Equal(actualOne, oneInput) {
		t.Fatal("the first CLI did not read the intended actual workspace role file")
	}
	if err := os.WriteFile(onePath, []byte("CHANGED_AFTER_COMPLETED_FORK\n"), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := os.ReadFile(onePath)
	if err != nil || string(changed) != "CHANGED_AFTER_COMPLETED_FORK\n" {
		t.Fatal("actual first role input file did not change after completed fork")
	}
	checkChild(oneResult, fileChild, roleOne)
	twoResult := run("FORK_INPUT_STDIN_C33", `cat ./role-two.txt | "$XBOT_BIN" bot-fork `+stdinChild+` --self --role-file - --json`)
	checkChild(twoResult, stdinChild, roleTwo)
	actualTwo, err := os.ReadFile(twoPath)
	if err != nil || !bytes.Equal(actualTwo, twoInput) {
		t.Fatal("stdin did not originate from the intended actual workspace role file")
	}
	if err := os.Remove(twoPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(twoPath); !os.IsNotExist(err) {
		t.Fatal("actual stdin input file still exists after removal")
	}
	checkChild(twoResult, stdinChild, roleTwo)
	personAfter, err := os.ReadFile(personPath)
	if err != nil || !bytes.Equal(personBefore, personAfter) {
		t.Fatal("role input forks changed the original shared person profile")
	}
	model.mu.Lock()
	archives := append([]cliBotForkArchiveRequest(nil), model.archive...)
	model.mu.Unlock()
	if len(archives) != 2 {
		t.Fatalf("two complete role inputs made %d actual archive calls", len(archives))
	}
	for i, request := range archives {
		role := roleOne
		if i == 1 {
			role = roleTwo
		}
		input := joined(request.stubRequest)
		if len(request.Tools) != 0 || !strings.Contains(input, role) || !strings.Contains(input, botFact) || !strings.Contains(input, "ROLE_INPUT_ACTUAL_HISTORY") {
			t.Fatal("actual noTools archive missed the complete role, persisted history or Bot fact")
		}
	}

	// Two direct writer boundaries after the positive core, without a broad
	// invalid-input matrix or synthetic missing-context/Compactor cases.
	snapshot := func() map[string][]byte {
		t.Helper()
		files := map[string][]byte{}
		for _, name := range []string{source, fileChild, stdinChild} {
			cfgPath := filepath.Join(srv.Root, "bots", name, "config.json")
			raw, err := os.ReadFile(cfgPath)
			if err != nil {
				t.Fatal(err)
			}
			files[cfgPath] = raw
			if err := filepath.WalkDir(filepath.Join(srv.Root, "bots", name, "memory"), func(path string, entry fs.DirEntry, err error) error {
				if err != nil || entry.IsDir() {
					return err
				}
				raw, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				files[path] = raw
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}
		return files
	}
	beforeReject := snapshot()
	rejectRole := func(cue, child, filename string, body []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(workspace, filename), body, 0600); err != nil {
			t.Fatal(err)
		}
		result := run(cue, `"$XBOT_BIN" bot-fork `+child+` --self --role-file `+shellQuote(filename)+` --json`)
		status, _, ok := strings.Cut(result, "\n")
		if !ok || !exitLine.MatchString(status) || status == "exit 0" {
			t.Fatal("invalid effective role did not produce a real nonzero CLI exit")
		}
		var reply cliBotForkReply
		if err := json.NewDecoder(strings.NewReader(commandOutput(result))).Decode(&reply); err != nil || reply.Created || reply.Stage != "validation" || reply.Error == "" || len(reply.Confirmed) != 0 || reply.UncertainMemory != nil {
			t.Fatal("invalid role claimed a child or committed fork stages")
		}
		if _, err := os.Stat(filepath.Join(srv.Root, "bots", child)); !os.IsNotExist(err) {
			t.Fatal("invalid role created an actual child directory")
		}
		model.mu.Lock()
		calls := len(model.archive)
		model.mu.Unlock()
		if calls != 2 || !reflect.DeepEqual(beforeReject, snapshot()) {
			t.Fatal("invalid role invoked the archive model or changed source/child config or memory")
		}
	}
	rejectRole("FORK_INPUT_EMPTY_D44", "ForkEmptyRoleDenied", "empty-role.txt", []byte(" \n\t "))
	rejectRole("FORK_INPUT_OVER_E55", "ForkLongRoleDenied", "long-role.txt", []byte(roleOne+"x"))
}
