//go:build smoke

package smoke

import (
	"strings"
	"testing"
)

// Exact0057 new source entry only. Existing offline fork SRC NAME and bg_
// branch lifecycle are separate caller domains, not aliases or baseline tests.
func TestCLIBotForkRuntimeEntry(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const source = "ForkEntrySource"
	srv.CreateBot(source, "Original actual source for the frozen bot-fork runtime entry.")
	const cue = "CLI_FORK_EXACT_ENTRY"
	model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Call the exact frozen0057 fork entry through the original source Shell", "command": `"$XBOT_BIN" bot-fork ForkEntryChild --self --role 'Review the explicit public project decisions.' --json`}))
	cliSkillSend(t, srv, source, cue)
	results := model.results(cue)
	if len(results) != 1 || !strings.HasPrefix(results[0], "exit 0\n") {
		t.Fatalf("actual source bot-fork entry did not succeed: %.1400q", results)
	}
}
