//go:build smoke

package smoke

import (
	"encoding/json"
	"strings"
	"testing"
)

// Current CLI spelling is flat `list`, not a new `bot list` family. This only
// establishes its absent source-aware JSON entry; concrete ADR0052 will define
// DTOs, visibility and the Primary-only create/delete acceptance separately.
func TestCLIBotRuntimeListEntry(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	const cue = "CLI_BOT_RUNTIME_LIST_ENTRY"
	model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Discover Bots through the original current source endpoint", "command": `"$XBOT_BIN" list --self --json`}))
	srv := Start(t, &Stub{srv: model.server})
	srv.CreateBot("BotListWorker", "Independent source runtime Bot list entry acceptance.")
	cliSkillSend(t, srv, "BotListWorker", cue)
	results := model.results(cue)
	if len(results) != 1 || !strings.HasPrefix(results[0], "exit 0\n") {
		t.Fatalf("actual source Bot list did not succeed: %.1024q", results)
	}
	raw := commandOutput(results[0])
	if !json.Valid([]byte(raw)) {
		t.Fatalf("actual source Bot list did not return complete runtime JSON: %.1024q", raw)
	}
}
