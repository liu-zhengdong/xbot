//go:build smoke

package smoke

import (
	"encoding/json"
	"strings"
	"testing"
)

// Only establish the absent runtime read entry with the real source Shell/BIN.
// CRUD DTOs, approval context and Timer semantics wait for concrete ADR0051;
// this does not invent a Timer, fake handler or second scheduling harness.
func TestCLIRoutineRuntimeListEntry(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	const cue = "CLI_ROUTINE_RUNTIME_LIST_ENTRY"
	model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "List routines through the original bound source", "command": `"$XBOT_BIN" routine list --self --json`}))
	srv := Start(t, &Stub{srv: model.server})
	srv.CreateBot("RoutineWorker", "Independent routine runtime list entry acceptance.")
	cliSkillSend(t, srv, "RoutineWorker", cue)
	results := model.results(cue)
	if len(results) != 1 || !strings.HasPrefix(results[0], "exit 0\n") {
		t.Fatalf("source runtime routine list did not succeed: %.1024q", results)
	}
	if !json.Valid([]byte(commandOutput(results[0]))) {
		t.Fatal("source runtime routine list did not return complete JSON")
	}
}
