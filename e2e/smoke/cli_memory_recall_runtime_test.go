//go:build smoke

package smoke

import (
	"strings"
	"testing"
)

// Frozen0059 entry only. Person scope, recursive files and read_file consumers
// belong to the first compiled candidate, not this unknown-namespace baseline.
func TestCLIMemoryRecallRuntimeEntry(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const source = "MemoryRecallEntrySource"
	srv.CreateBot(source, "Find the explicit public deployment note through the original current source.")
	const cue = "CLI_MEMORY_RECALL_EXACT_ENTRY"
	model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Call exact frozen memory-recall from this actual source Shell with its original CallID", "command": `"$XBOT_BIN" memory-recall --self --query 'public deployment note' --json`}))
	cliSkillSend(t, srv, source, cue)
	results := model.results(cue)
	if len(results) != 1 || !strings.HasPrefix(results[0], "exit 0\n") {
		t.Fatalf("actual source memory-recall entry did not succeed: %.1400q", results)
	}
}
