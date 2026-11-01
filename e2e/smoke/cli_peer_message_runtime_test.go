//go:build smoke

package smoke

import (
	"strings"
	"testing"
)

// Only the exact0056 local source caller, separate from owner message/SSE and
// external-agent send. The full paired-device branch remains required later.
func TestCLIPeerMessageRuntimeEntry(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const source, target = "PeerEntrySource", "PeerEntryTarget"
	srv.CreateBot(source, "Actual self source for the peer-message frozen local entry.")
	srv.CreateBot(target, "Receive the original same-owner Bot inbox event as untrusted data.")
	const cue = "CLI_PEER_EXACT_ENTRY"
	model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Send through the exact frozen0056 local source CLI entry", "command": `"$XBOT_BIN" peer-message ` + target + ` --self --text PEER_LOCAL_EXACT_ENTRY --json`}))
	cliSkillSend(t, srv, source, cue)
	results := model.results(cue)
	if len(results) != 1 || !strings.HasPrefix(results[0], "exit 0\n") {
		t.Fatalf("actual source peer-message entry did not succeed: %.1400q", results)
	}
}
