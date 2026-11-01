//go:build smoke

package smoke

import (
	"strings"
	"testing"
)

// Baseline only the frozen0055 chat-group namespace; owner sidebar group and
// owner HTTP/SSE message/send commands are separate existing caller domains.
func TestCLIChatGroupRuntimeListEntry(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const source = "ChatGroupEntryWorker"
	srv.CreateBot(source, "Original member-source duties for chat-group readonly CLI entry.")
	const cue = "CLI_CHAT_GROUP_ENTRY"
	model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Read source member chat groups through the exact frozen0055 CLI", "command": `"$XBOT_BIN" chat-group list --self --json`}))
	cliSkillSend(t, srv, source, cue)
	results := model.results(cue)
	if len(results) != 1 || !strings.HasPrefix(results[0], "exit 0\n") {
		t.Fatalf("actual source chat-group readonly entry did not succeed: %.1400q", results)
	}
}
