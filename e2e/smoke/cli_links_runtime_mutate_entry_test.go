//go:build smoke

package smoke

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Entry for the links install/remove runtime CLI: the noun now accepts both
// subcommands from a genuinely prepared nonprimary source Native Shell call.
// Each one reaches the runtime owner-card stage, so the command outlives its
// two-second foreground window and lands on a real background shell job
// instead of the retired parser rejection. Business semantics have their own
// samecut in cli_links_runtime_mutate_test.go.
func TestCLILinksRuntimeMutateEntry(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const source = "LinksMutateEntrySource"
	srv.CreateBot(source, "Install or remove a connector through the original source Native Shell CLI.")
	var primary struct{ Name string }
	primaryRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil || json.Unmarshal(primaryRaw, &primary) != nil || primary.Name == "" || primary.Name == source {
		t.Fatal("links mutate entry source must be actually nonprimary")
	}
	for _, c := range []struct {
		name, command string
	}{
		{"install", `"$XBOT_BIN" links install --self telegram --json`},
		{"remove", `"$XBOT_BIN" links remove --self telegram --json`},
	} {
		result := cliLinksRun(t, model, srv, source, "LINKS_MUTATE_ENTRY_"+strings.ToUpper(c.name), c.command)
		if strings.HasPrefix(result, "exit 0\n") {
			t.Fatalf("links %s silently succeeded before its owner card: %.900q", c.name, result)
		}
		if strings.Contains(commandOutput(result), `links: unexpected subcommand`) {
			t.Fatalf("links %s kept the retired parser rejection: %.900q", c.name, result)
		}
		if !strings.HasPrefix(result, "background sh_") {
			t.Fatalf("links %s did not reach the owner-card stage on a live Shell job: %.900q", c.name, result)
		}
	}
}
