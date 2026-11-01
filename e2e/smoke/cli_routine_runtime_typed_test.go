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

// One typed/time lifecycle through real source Shell/BIN calls. Prechecks stay
// empty throughout; this case neither invents approval context nor seeds Timer
// internals. Actual definition bytes guard rejected merged/unknown-field writes.
func TestCLIRoutineRuntimeTypedOnceLifecycle(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const worker, id = "RoutineTypedWorker", "runtime-typed-once"
	srv.CreateBot(worker, "Independent routine typed and time acceptance.")
	definition := filepath.Join(srv.Root, "bots", worker, "routines", id+".json")

	// A comfortably future offset timestamp avoids the two-minute boundary and
	// scheduler delivery. The saved instant must use its canonical UTC form.
	future := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	offsetAt := future.In(time.FixedZone("fixture-offset", 5*60*60+30*60)).Format(time.RFC3339)
	utcAt := future.UTC().Format(time.RFC3339)
	createArgs, err := json.Marshal(map[string]any{"id": id, "name": "Typed once", "prompt": "  TYPED_INITIAL_PROMPT \n", "at": "  " + offsetAt + "  ", "precheck": "", "skills": []string{" first-reference ", "second-reference", "first-reference"}})
	if err != nil {
		t.Fatal(err)
	}
	mutateAndList := func(cue, action, args string) cliRoutineView {
		t.Helper()
		model.add(cue,
			mcpRuntimeTool("sh", map[string]string{"description": "Apply typed routine fields through the actual source Timer", "command": `"$XBOT_BIN" routine ` + action + ` --self --args ` + shellQuote(args) + ` --json`}),
			mcpRuntimeTool("sh", map[string]string{"description": "Observe the complete current routine definition", "command": `"$XBOT_BIN" routine list --self --json`}),
		)
		cliSkillSend(t, srv, worker, cue)
		results := model.results(cue)
		var receipt cliRoutineWriteReply
		cliRoutineDecode(t, results[0], &receipt)
		if receipt.ID != id || !receipt.Applied || receipt.Stage != "complete" || receipt.Error != "" {
			t.Fatal("typed mutation did not report the actual complete change")
		}
		var list cliRoutineListReply
		cliRoutineDecode(t, results[1], &list)
		if list.Routines == nil || len(*list.Routines) != 1 || list.Count != 1 || list.File != "" || list.Error != "" {
			t.Fatal("typed mutation did not leave exactly one complete inline routine")
		}
		view := (*list.Routines)[0]
		raw, err := os.ReadFile(definition)
		if err != nil {
			t.Fatal(err)
		}
		var saved cliRoutineView
		if err := json.Unmarshal(raw, &saved); err != nil {
			t.Fatalf("actual routine definition is not complete JSON: %v", err)
		}
		if saved.ID != view.ID || saved.Name != view.Name || saved.Prompt != view.Prompt || saved.Cron != view.Cron || saved.At != view.At || saved.Enabled != view.Enabled || saved.Precheck != view.Precheck || saved.LastSlot != view.LastSlot || !slices.Equal(saved.Skills, view.Skills) {
			t.Fatal("runtime list disagrees with the actual saved typed routine")
		}
		return view
	}

	initial := mutateAndList("CLI_ROUTINE_TYPED_CREATE_ONCE", "create", string(createArgs))
	if initial.ID != id || initial.Name != "Typed once" || initial.Prompt != "TYPED_INITIAL_PROMPT" || initial.At != utcAt || initial.Zone != "" || initial.Cron != "" || !initial.Enabled || initial.Precheck != "" || initial.LastSlot != "" || !slices.Equal(initial.Skills, []string{"first-reference", "second-reference", "first-reference"}) {
		t.Fatal("create lost offset-to-UTC, default-enabled, trimmed prompt or ordered skills semantics")
	}
	preserved := mutateAndList("CLI_ROUTINE_TYPED_NULL_PRESERVE", "update "+id, `{"name":" \t ","prompt":" \r\n ","cron":" ","enabled":null,"precheck":null,"skills":null}`)
	if preserved.Name != initial.Name || preserved.Prompt != initial.Prompt || preserved.At != initial.At || preserved.Cron != initial.Cron || preserved.Zone != initial.Zone || preserved.Enabled != initial.Enabled || preserved.Precheck != initial.Precheck || preserved.LastSlot != initial.LastSlot || !slices.Equal(preserved.Skills, initial.Skills) {
		t.Fatal("null pointers or blank text changed preserved routine fields")
	}
	cleared := mutateAndList("CLI_ROUTINE_TYPED_EXPLICIT_CLEAR", "update "+id, `{"name":"  Keep name spacing  ","prompt":"  TYPED_UPDATED_PROMPT\n ","enabled":false,"precheck":"","skills":[]}`)
	if cleared.Name != "  Keep name spacing  " || cleared.Prompt != "TYPED_UPDATED_PROMPT" || cleared.Enabled || cleared.Precheck != "" || len(cleared.Skills) != 0 || cleared.At != initial.At || cleared.Cron != initial.Cron || cleared.Zone != initial.Zone || cleared.LastSlot != initial.LastSlot {
		t.Fatal("explicit false/empty fields or nonblank text rules were not preserved")
	}
	omitted := mutateAndList("CLI_ROUTINE_TYPED_OMITTED_PRESERVE", "update "+id, `{"name":"  ","prompt":"\t","cron":" "}`)
	if omitted.Name != cleared.Name || omitted.Prompt != cleared.Prompt || omitted.Enabled != cleared.Enabled || omitted.Precheck != cleared.Precheck || !slices.Equal(omitted.Skills, cleared.Skills) || omitted.At != cleared.At || omitted.Cron != cleared.Cron || omitted.Zone != cleared.Zone || omitted.LastSlot != cleared.LastSlot {
		t.Fatal("omitted pointers or blank text replaced previously saved fields")
	}

	rejectUnchanged := func(cue, action, args, absentID string) {
		t.Helper()
		before, err := os.ReadFile(definition)
		if err != nil {
			t.Fatal(err)
		}
		model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Reject an invalid typed routine before changing its definition", "command": `"$XBOT_BIN" routine ` + action + ` --self --args ` + shellQuote(args) + ` --json`}))
		cliSkillSend(t, srv, worker, cue)
		result := model.results(cue)[0]
		status, _, ok := strings.Cut(result, "\n")
		if !ok || !exitLine.MatchString(status) || status == "exit 0" || len(commandOutput(result)) > 64<<10 || strings.TrimSpace(commandOutput(result)) == "" {
			t.Fatal("invalid typed routine did not return a bounded nonzero CLI diagnostic")
		}
		after, err := os.ReadFile(definition)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, before) {
			t.Fatal("rejected typed request changed the exact committed routine definition")
		}
		if absentID != "" {
			if _, err := os.Stat(filepath.Join(filepath.Dir(definition), absentID+".json")); !os.IsNotExist(err) {
				t.Fatal("rejected create left a routine definition on disk")
			}
		}
	}
	// A once already has At. Updating Cron must validate the freshly merged
	// invariant instead of silently clearing At or accepting two schedules.
	rejectUnchanged("CLI_ROUTINE_TYPED_ONCE_CRON_CONFLICT", "update "+id, `{"cron":"0 9 * * *"}`, "")
	rejectUnchanged("CLI_ROUTINE_TYPED_UPDATE_AT_REJECTED", "update "+id, `{"at":"`+utcAt+`"}`, "")
	rejectUnchanged("CLI_ROUTINE_TYPED_UPDATE_ZONE_REJECTED", "update "+id, `{"zone":"UTC"}`, "")
	rejectUnchanged("CLI_ROUTINE_TYPED_UPDATE_SLOT_REJECTED", "update "+id, `{"last_slot":"forbidden-client-slot"}`, "")
	rejectUnchanged("CLI_ROUTINE_TYPED_MISSING_SCHEDULE", "create", `{"id":"typed-missing-schedule","prompt":"Must not create without a schedule"}`, "typed-missing-schedule")
	pastAt := time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339)
	pastArgs, err := json.Marshal(map[string]string{"id": "typed-past-once", "prompt": "Must not create a stale once", "at": " \t" + pastAt + "\n "})
	if err != nil {
		t.Fatal(err)
	}
	rejectUnchanged("CLI_ROUTINE_TYPED_WHITESPACE_PAST_AT", "create", string(pastArgs), "typed-past-once")

	// Explicit same-ID create replaces the definition and restores create's
	// Enabled=true semantics, without manufacturing another routine identity.
	overwriteArgs, err := json.Marshal(map[string]any{"id": id, "name": "Same ID replacement", "prompt": "TYPED_OVERWRITE_PROMPT", "at": offsetAt, "precheck": ""})
	if err != nil {
		t.Fatal(err)
	}
	overwritten := mutateAndList("CLI_ROUTINE_TYPED_SAME_ID_OVERWRITE", "create", string(overwriteArgs))
	if overwritten.ID != id || overwritten.Name != "Same ID replacement" || overwritten.Prompt != "TYPED_OVERWRITE_PROMPT" || overwritten.At != utcAt || overwritten.Cron != "" || !overwritten.Enabled || overwritten.Precheck != "" || len(overwritten.Skills) != 0 || overwritten.LastSlot != "" {
		t.Fatal("same-ID create did not replace the definition with normal create semantics")
	}
}
