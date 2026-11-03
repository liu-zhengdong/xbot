//go:build smoke

package smoke

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Entry baseline for the links install/remove runtime CLI. Until the noun
// grows these subcommands, the original parser layer keeps rejecting them
// from a genuinely prepared nonprimary source Native Shell call. Once the
// implementation lands, each command keeps its own real semantics instead of
// this parser rejection; that first candidate owns the green version.
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
		name, command, want string
	}{
		{"install", `"$XBOT_BIN" links install --self telegram --json`, `links: unexpected subcommand "install"`},
		{"remove", `"$XBOT_BIN" links remove --self telegram --json`, `links: unexpected subcommand "remove"`},
	} {
		result := cliLinksRun(t, model, srv, source, "LINKS_MUTATE_ENTRY_"+strings.ToUpper(c.name), c.command)
		if strings.HasPrefix(result, "exit 0\n") {
			t.Fatalf("links %s silently succeeded before its implementation: %.900q", c.name, result)
		}
		if !strings.Contains(commandOutput(result), c.want) {
			t.Fatalf("links %s lost its original parser rejection %q: %.900q", c.name, c.want, result)
		}
	}
}
