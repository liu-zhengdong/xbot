//go:build smoke

package smoke

import (
	"strings"
	"testing"
)

// Frozen0061 namespace only. Real compact.LLM usage, Block/environment ordering
// and late errors belong to the first candidate, not this entry-only baseline.
func TestCLICompactRuntimeEntry(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const source = "CompactEntrySource"
	srv.CreateBot(source, "Compress a finished original source transcript range through the native Shell CLI.")
	const cue = "CLI_COMPACT_EXACT_ENTRY"
	model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Call the exact frozen compact namespace from the original source Native Shell", "command": `"$XBOT_BIN" compact --self --from 1 --to 1 --json`}))
	cliSkillSend(t, srv, source, cue)
	results := model.results(cue)
	if len(results) != 1 || !strings.HasPrefix(results[0], "exit 0\n") {
		t.Fatalf("actual source compact entry did not succeed: %.1400q", results)
	}
}
