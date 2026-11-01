//go:build smoke

package smoke

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Only establish the concrete0054 caller at a genuine nonprimary source.
// Save/next-wake DTO and file boundaries are fixed separately after shape PASS.
func TestCLIPersonaRuntimeEntry(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const source = "PersonaEntryWorker"
	srv.CreateBot(source, "Original nonprimary duties before source persona CLI replacement.")
	raw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var primary struct{ Name string }
	if err := json.Unmarshal(raw, &primary); err != nil || primary.Name == "" || primary.Name == source {
		t.Fatal("actual source must be a nonprimary Bot")
	}
	const warm = "CLI_PERSONA_ENTRY_WARM"
	model.add(warm, mcpRuntimeTool("sh", map[string]string{"description": "Warm the actual source and its original default card assembly", "command": `printf 'persona-source-warmed\n'`}))
	cliSkillSend(t, srv, source, warm)
	if got := model.results(warm); len(got) != 1 || !strings.HasPrefix(got[0], "exit 0\n") {
		t.Fatal("actual source did not complete its initial warm wake")
	}
	const cue = "CLI_PERSONA_ENTRY"
	model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Replace this nonprimary source's complete duties through the frozen0054 caller", "command": `"$XBOT_BIN" persona set --self --text 'CLI_PERSONA_ENTRY_DUTY' --json`}))
	cliSkillSend(t, srv, source, cue)
	results := model.results(cue)
	if len(results) != 1 || !strings.HasPrefix(results[0], "exit 0\n") {
		t.Fatalf("actual nonprimary source persona entry did not succeed: %.1400q", results)
	}
}
