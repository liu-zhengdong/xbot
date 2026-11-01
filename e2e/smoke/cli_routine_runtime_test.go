//go:build smoke

package smoke

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Only establish the absent runtime read entry with the real source Shell/BIN.
// This sealed baseline predates contract3e3067d4; the first product candidate
// uses CRUDEntry below, without repeating this accepted missing-self red.
func TestCLIRoutineRuntimeListEntry(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	const cue = "CLI_ROUTINE_RUNTIME_LIST_ENTRY"
	model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "List routines through the original bound source", "command": `"$XBOT_BIN" routine list --self --json`}))
	srv := Start(t, &Stub{srv: model.server})
	srv.CreateBot("RoutineWorker", "Independent routine runtime list entry acceptance.")
	cliSkillSend(t, srv, "RoutineWorker", cue)
	results := model.results(cue)
	if len(results) != 1 || !strings.HasPrefix(results[0], "exit 0\n") {
		t.Fatalf("source runtime routine list did not succeed: %.1024q", results)
	}
	if !json.Valid([]byte(commandOutput(results[0]))) {
		t.Fatal("source runtime routine list did not return complete JSON")
	}
}

// First candidate gate: real mounted source Timer CRUD, with no precheck and
// hence no invented approval bridge. Extra reads verify update/delete effects.
func TestCLIRoutineRuntimeCRUDEntry(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	const cue, worker, id = "CLI_ROUTINE_RUNTIME_CRUD_ENTRY", "RoutineCRUDWorker", "runtime-crud-entry"
	// Keep the scheduler out of this CRUD-only chain using tomorrow's local
	// calendar slot; a separate lifecycle case will exercise real delivery.
	cron := time.Now().Add(24 * time.Hour).Format("4 15 2 1 *")
	createJSON, _ := json.Marshal(map[string]any{"id": id, "name": "Runtime entry", "prompt": "ROUTINE_CRUD_INITIAL", "cron": cron, "skills": []string{"missing-is-allowed", "missing-is-allowed"}})
	create := string(createJSON)
	patch := `{"name":"Updated runtime entry","prompt":"ROUTINE_CRUD_UPDATED","enabled":false,"skills":[]}`
	model.add(cue,
		mcpRuntimeTool("sh", map[string]string{"description": "Create a routine through the current source Timer", "command": `"$XBOT_BIN" routine create --self --args ` + shellQuote(create) + ` --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "List the newly saved routine from the same source", "command": `"$XBOT_BIN" routine list --self --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Update the same routine through a complete args file", "command": "printf '%s' " + shellQuote(patch) + ` > routine-entry-patch.json && "$XBOT_BIN" routine update ` + id + ` --self --args-file ./routine-entry-patch.json --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Observe the applied routine changes", "command": `"$XBOT_BIN" routine list --self --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Delete the original routine through the same mounted Timer", "command": `"$XBOT_BIN" routine delete ` + id + ` --self --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Verify the routine is absent after deletion", "command": `"$XBOT_BIN" routine list --self --json`}),
	)
	srv := Start(t, &Stub{srv: model.server})
	srv.CreateBot(worker, "Independent runtime routine CRUD acceptance.")
	cliSkillSend(t, srv, worker, cue)
	results := model.results(cue)
	if len(results) != 6 {
		t.Fatalf("routine CRUD result count=%d", len(results))
	}
	for _, step := range []int{0, 2, 4} {
		var receipt cliRoutineWriteReply
		cliRoutineDecode(t, results[step], &receipt)
		if receipt.ID != id || !receipt.Applied || receipt.Stage != "complete" || receipt.Error != "" {
			t.Fatalf("routine CRUD step%d did not report the actual complete change: %+v", step, receipt)
		}
	}
	var created cliRoutineListReply
	cliRoutineDecode(t, results[1], &created)
	if created.Routines == nil || len(*created.Routines) != 1 || created.Count != 1 || created.File != "" || created.Error != "" {
		t.Fatal("created routine was not completely visible in actual small list")
	}
	before := (*created.Routines)[0]
	if before.ID != id || before.Name != "Runtime entry" || before.Prompt != "ROUTINE_CRUD_INITIAL" || before.Cron != cron || before.Zone != "local" || before.At != "" || !before.Enabled || before.Precheck != "" || before.LastSlot != "" || len(before.Skills) != 2 || before.Skills[0] != "missing-is-allowed" || before.Skills[1] != "missing-is-allowed" {
		t.Fatal("create lost cron-local/default-enabled/ordered duplicate skill semantics")
	}
	var updated cliRoutineListReply
	cliRoutineDecode(t, results[3], &updated)
	if updated.Routines == nil || len(*updated.Routines) != 1 || updated.Count != 1 || updated.File != "" || updated.Error != "" {
		t.Fatal("updated routine was not visible in actual list")
	}
	after := (*updated.Routines)[0]
	if after.ID != id || after.Name != "Updated runtime entry" || after.Prompt != "ROUTINE_CRUD_UPDATED" || after.Enabled || len(after.Skills) != 0 || after.Cron != before.Cron || after.Zone != before.Zone || after.At != before.At || after.LastSlot != before.LastSlot || after.Precheck != "" {
		t.Fatal("update lost explicit false/empty skills or changed unrequested schedule fields")
	}
	var deleted cliRoutineListReply
	cliRoutineDecode(t, results[5], &deleted)
	if deleted.Routines == nil || len(*deleted.Routines) != 0 || deleted.Count != 0 || deleted.File != "" || deleted.Error != "" {
		t.Fatal("delete did not leave an actual complete empty list")
	}
	if _, err := os.Stat(filepath.Join(srv.Root, "bots", worker, "routines", id+".json")); !os.IsNotExist(err) {
		t.Fatal("deleted routine definition remains on disk")
	}
}
