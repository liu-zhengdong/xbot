//go:build smoke

package smoke

import (
	"strings"
	"testing"
)

// Frozen0060 namespace only. Stable tool seq, hydrated expansion and Detail
// consumers belong to the first compiled candidate, not this entry baseline.
func TestCLITranscriptRuntimeEntry(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const source = "TranscriptEntrySource"
	srv.CreateBot(source, "Read the original recorded deployment evidence through the current source transcript CLI.")
	const cue = "CLI_TRANSCRIPT_EXACT_ENTRY"
	model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Call the exact frozen transcript search namespace from the original source Native Shell", "command": `"$XBOT_BIN" transcript search --self --query 'recorded deployment evidence' --json`}))
	cliSkillSend(t, srv, source, cue)
	results := model.results(cue)
	if len(results) != 1 || !strings.HasPrefix(results[0], "exit 0\n") {
		t.Fatalf("actual source transcript entry did not succeed: %.1400q", results)
	}
}
