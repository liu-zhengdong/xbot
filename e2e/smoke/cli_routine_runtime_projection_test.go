//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Only the upstream model is scripted. The large definition is created through
// the actual mounted Timer CLI, and list reaches the model through unredirected
// native Shell output with its existing 32KiB budget.
func TestCLIRoutineRuntimeLargeListCurrentRoute(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const original, current = "RoutineProjectionWorker", "RoutineProjectionMoved"
	const id, name = "runtime-projection", "Complete projected routine"
	srv.CreateBot(original, "Independent runtime routine list projection acceptance.")
	workspace := filepath.Join(srv.Root, "work", original)
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}

	prompt := "ROUTINE_PROJECTION_BEGIN\n" + strings.Repeat("routine projection line\n", 1800) + "ROUTINE_PROJECTION_FULL_TAIL"
	encodedPrompt, err := json.Marshal(prompt)
	if err != nil {
		t.Fatal(err)
	}
	if len(encodedPrompt) <= 32<<10 || len(encodedPrompt) >= 60<<10 {
		t.Fatal("projection fixture must exceed native Shell output while fitting the HTTP reply budget")
	}
	// Tomorrow's local calendar slot keeps this case about CRUD/projection;
	// actual delivery and approval behavior have separate acceptance coverage.
	cron := time.Now().Add(24 * time.Hour).Format("4 15 2 1 *")
	skills := []string{"projection-first", "projection-second"}
	args, err := json.Marshal(map[string]any{"id": id, "name": name, "prompt": prompt, "cron": cron, "skills": skills})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "routine-projection-args.json"), args, 0600); err != nil {
		t.Fatal(err)
	}

	const cue = "CLI_ROUTINE_LARGE_LIST_CURRENT_ROUTE"
	model.add(cue,
		mcpRuntimeTool("sh", map[string]string{"description": "Create a large routine through the actual mounted Timer", "command": `"$XBOT_BIN" routine create --self --args-file ./routine-projection-args.json --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Move the source before projecting its routine list", "command": `"$XBOT_BIN" profile set --name ` + current + ` --self --json`}),
		// No redirect or pipe: a protocol-valid but clipped 40KiB inline result
		// must fail strict decoding here, rather than bypassing the consumer.
		mcpRuntimeTool("sh", map[string]string{"description": "List routines through the original source into bounded Shell output", "command": `XBOT_BOT=` + shellQuote(original) + ` "$XBOT_BIN" routine list --self --json`}),
	)
	cliSkillSend(t, srv, original, cue)
	results := model.results(cue)
	if len(results) != 3 {
		t.Fatalf("routine projection result count=%d", len(results))
	}
	var created cliRoutineWriteReply
	cliRoutineDecode(t, results[0], &created)
	if created.ID != id || !created.Applied || created.Stage != "complete" || created.Error != "" {
		t.Fatal("large argsfile create did not report a complete actual Timer mutation")
	}
	var renamed map[string]any
	cliSkillDecode(t, results[1], &renamed)
	var listed cliRoutineListReply
	cliRoutineDecode(t, results[2], &listed)
	if len(commandOutput(results[2])) > 32<<10 {
		t.Fatal("final list JSON exceeded the actual native Shell output budget")
	}
	if listed.Count != 1 || listed.File == "" || listed.Access != "local" || listed.Bytes <= 0 || listed.Error != "" {
		t.Fatal("large list omitted its complete count, actual local artifact or byte count")
	}
	if listed.Routines != nil && len(*listed.Routines) != 0 {
		t.Fatal("large list presented a partial inline definition as a complete list")
	}
	if filepath.IsAbs(listed.File) {
		t.Fatal("routine list artifact must be relative to the current workspace")
	}
	rel := filepath.Clean(filepath.FromSlash(listed.File))
	if !strings.HasPrefix(rel, "results"+string(filepath.Separator)) {
		t.Fatal("routine list did not use the existing workspace results directory")
	}
	artifactPath := filepath.Join(srv.Root, "work", current, rel)
	info, err := os.Stat(artifactPath)
	if err != nil {
		t.Fatalf("stat actual routine artifact in the current workspace: %v", err)
	}
	if !info.Mode().IsRegular() || info.Size() != listed.Bytes || info.Size() > 20<<20 {
		t.Fatal("routine artifact byte count or existing result budget was not preserved")
	}
	artifact, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(artifact)) != listed.Bytes {
		t.Fatalf("actual routine artifact length=%d, advertised=%d", len(artifact), listed.Bytes)
	}

	// The contract requires complete JSON without fixing an artifact envelope.
	// Accept a complete views array or complete list DTO; both must preserve
	// exactly one full definition, rather than merely a preview or prompt tail.
	var routines []cliRoutineView
	if bytes.HasPrefix(bytes.TrimSpace(artifact), []byte("[")) {
		if err := json.Unmarshal(artifact, &routines); err != nil {
			t.Fatalf("routine artifact must be one complete JSON list: %v", err)
		}
	} else {
		var saved cliRoutineListReply
		if err := json.Unmarshal(artifact, &saved); err != nil {
			t.Fatalf("routine artifact must be one complete JSON reply: %v", err)
		}
		if saved.Routines == nil || saved.Count != 1 || saved.Error != "" {
			t.Fatal("routine artifact reply omitted the complete definitions")
		}
		routines = *saved.Routines
	}
	if len(routines) != 1 {
		t.Fatalf("complete routine artifact definition count=%d", len(routines))
	}
	got := routines[0]
	if got.ID != id || got.Name != name || got.Prompt != prompt || got.Cron != cron || got.At != "" || got.Zone != "local" || !got.Enabled || got.Precheck != "" || got.LastSlot != "" || !slices.Equal(got.Skills, skills) {
		t.Fatal("routine artifact did not preserve the full prompt tail and complete metadata")
	}
	if _, err := os.Stat(filepath.Join(srv.Root, "bots", current, "routines", id+".json")); err != nil {
		t.Fatalf("actual routine definition is absent from the current Bot directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(srv.Root, "bots", original, "routines")); !os.IsNotExist(err) {
		t.Fatal("routine list revived or retained the previous routines directory")
	}
	if _, err := os.Stat(filepath.Join(srv.Root, "work", original)); !os.IsNotExist(err) {
		t.Fatal("routine list revived or retained the previous workspace")
	}
}
