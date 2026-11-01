//go:build smoke

package smoke

import (
	"strings"
	"testing"
)

// Frozen0058 source entry only; SQLite consumers belong to the first candidate
// chain, not to this pre-implementation unknown-namespace baseline.
func TestCLIExperienceRuntimeEntry(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const source = "ExperienceEntrySource"
	srv.CreateBot(source, "Record the explicit project decision through the original current source.")
	const cue = "CLI_EXPERIENCE_EXACT_ENTRY"
	model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Call exact frozen experience record from this actual source Shell", "command": `"$XBOT_BIN" experience record --self --summary 'Confirmed the public deployment decision.' --json`}))
	cliSkillSend(t, srv, source, cue)
	results := model.results(cue)
	if len(results) != 1 || !strings.HasPrefix(results[0], "exit 0\n") {
		t.Fatalf("actual source experience entry did not succeed: %.1400q", results)
	}
}
