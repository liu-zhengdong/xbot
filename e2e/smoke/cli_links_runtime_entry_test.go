//go:build smoke

package smoke

import (
	"strings"
	"testing"
)

// ADR0062 namespace only. Actual catalog TSV, load semantics, connector/MCP rows
// and secrets scrubbing belong to the first candidate, not this entry-only baseline.
func TestCLILinksRuntimeEntry(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const source = "LinksEntrySource"
	srv.CreateBot(source, "Search the connector catalog through the native Shell CLI.")
	const cue = "CLI_LINKS_EXACT_ENTRY"
	model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Call the exact frozen links namespace from the original source Native Shell", "command": `"$XBOT_BIN" links search --self --json`}))
	cliSkillSend(t, srv, source, cue)
	results := model.results(cue)
	if len(results) != 1 || !strings.HasPrefix(results[0], "exit 0\n") {
		t.Fatalf("actual source links entry did not succeed: %.1400q", results)
	}
}
