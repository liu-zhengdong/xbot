//go:build smoke

package smoke

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Existing CLI spelling is flat pin NAME, not a new control command family.
// Only establish the missing --self entry; ADR0053 will fix DTOs and A->B
// primary transfer receipts/qualification separately after its shape review.
func TestCLIBotControlRuntimePinEntry(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const target = "PinEntryPeer"
	srv.CreateBot(target, "Actual authenticated peer for the narrow pin runtime entry.")
	raw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var primary struct{ Name string }
	if err := json.Unmarshal(raw, &primary); err != nil || primary.Name == "" {
		t.Fatal("could not resolve the actual Primary source")
	}
	readOwners := func(name string) []string {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(srv.Root, "bots", name, "config.json"))
		if err != nil {
			t.Fatal(err)
		}
		var config struct{ Owners []string }
		if err := json.Unmarshal(raw, &config); err != nil {
			t.Fatal(err)
		}
		return config.Owners
	}
	shared := false
	targetOwners := readOwners(target)
	for _, owner := range readOwners(primary.Name) {
		shared = shared || owner != "" && slices.Contains(targetOwners, owner)
	}
	if !shared {
		t.Fatal("actual Primary and peer lack this run's authenticated owner intersection")
	}
	const cue = "CLI_BOT_CONTROL_PIN_ENTRY"
	model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Pin a real visible peer from the actual current Primary source", "command": `"$XBOT_BIN" pin ` + target + ` --self`}))
	cliSkillSend(t, srv, primary.Name, cue)
	results := model.results(cue)
	if len(results) != 1 || !strings.HasPrefix(results[0], "exit 0\n") {
		t.Fatalf("actual source pin runtime entry did not succeed: %.1024q", results)
	}
}
