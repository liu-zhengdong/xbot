//go:build smoke

package smoke

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Real source precheck context and real mounted Timer delivery. This does not
// turn production's permissive assembly into a synthetic strict review Kernel;
// original approval/park/child-policy fault seams remain implementation units.
func TestCLIRoutineRuntimeCurrentTimerAndGate(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const original, moved, id = "RoutineLiveWorker", "RoutineLiveMoved", "runtime-live-check"
	const wake = "CLI_ROUTINE_ACTUAL_TIMER_WAKE"
	model.add(wake)
	srv.CreateBot(original, "Independent actual Timer/source context/live gate acceptance.")
	proof := filepath.Join(srv.Root, "precheck-proof.txt")
	at := time.Now().Add(3 * time.Minute).UTC()
	createJSON, _ := json.Marshal(map[string]any{"id": id, "name": "Live timer", "prompt": wake, "at": at.Format(time.RFC3339), "precheck": "printf checked >> " + shellQuote(proof)})
	model.add("CLI_ROUTINE_LIVE_CREATE", mcpRuntimeTool("sh", map[string]string{"description": "Create a prechecked once routine from the real Shell Job context", "command": `"$XBOT_BIN" routine create --self --args ` + shellQuote(string(createJSON)) + ` --json`}))
	cliSkillSend(t, srv, original, "CLI_ROUTINE_LIVE_CREATE")
	var created cliRoutineWriteReply
	cliRoutineDecode(t, model.results("CLI_ROUTINE_LIVE_CREATE")[0], &created)
	if created.ID != id || !created.Applied || created.Stage != "complete" || created.Error != "" {
		t.Fatal("actual source precheck context did not save the routine")
	}
	// The same original environment must retain the captured cached Timer when
	// its Resources move. Update omits precheck, so it reviews the stored command.
	model.add("CLI_ROUTINE_LIVE_RENAME", mcpRuntimeTool("sh", map[string]string{"description": "Keep the actual Timer through source rename and retained-precheck update", "command": `"$XBOT_BIN" profile set --name ` + moved + ` --self --json > routine-rename.json && "$XBOT_BIN" routine update ` + id + ` --self --args '{"name":"Moved timer"}' --json`}))
	cliSkillSend(t, srv, original, "CLI_ROUTINE_LIVE_RENAME")
	var updated cliRoutineWriteReply
	cliRoutineDecode(t, model.results("CLI_ROUTINE_LIVE_RENAME")[0], &updated)
	if !updated.Applied || updated.ID != id || updated.Stage != "complete" || updated.Error != "" {
		t.Fatal("captured Timer or retained original precheck context did not follow rename")
	}
	definition := filepath.Join(srv.Root, "bots", moved, "routines", id+".json")
	before, err := os.ReadFile(definition)
	if err != nil {
		t.Fatal(err)
	}
	model.add("CLI_ROUTINE_MISSING_CONTEXT", mcpRuntimeTool("sh", map[string]string{"description": "Reject a retained precheck without an actual live source call", "command": `XBOT_CALL_ID=missing "$XBOT_BIN" routine update ` + id + ` --self --args '{"name":"Must not save"}' --json`}))
	cliSkillSend(t, srv, moved, "CLI_ROUTINE_MISSING_CONTEXT")
	missing := model.results("CLI_ROUTINE_MISSING_CONTEXT")[0]
	if strings.HasPrefix(missing, "exit 0\n") || !strings.Contains(strings.ToLower(commandOutput(missing)), "context") {
		t.Fatal("missing original source call context was accepted or not explained")
	}
	after, err := os.ReadFile(definition)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("missing approval context changed the actual routine definition")
	}

	// Use the preserved user CLI to change the live machine switch. No review
	// callback or Timer hook is injected; the serve ticker reads actual state.
	localCLI := func(args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, xbotBinary, append([]string{"--root", srv.Root}, args...)...)
		cmd.Env = srv.env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("local control CLI failed: %v; %.256q", err, out)
		}
	}
	localCLI("permissions", "on")
	localCLI("machine", "set", "local", "off")
	srv.Advance(190 * time.Second)
	readSaved := func() cliRoutineView {
		t.Helper()
		raw, err := os.ReadFile(definition)
		if err != nil {
			t.Fatal(err)
		}
		var routine cliRoutineView
		if err := json.Unmarshal(raw, &routine); err != nil {
			t.Fatal(err)
		}
		return routine
	}
	WaitFor(t, 10*time.Second, "real live-off precheck consumes once slot", func() (bool, string) {
		routine := readSaved()
		return routine.LastSlot != "" && !routine.Enabled, "once slot not yet committed"
	})
	if _, err := os.Stat(proof); !os.IsNotExist(err) {
		t.Fatal("live off gate executed the precheck")
	}
	for _, event := range mcpRuntimeInbox(t, srv, moved) {
		if strings.Contains(event["content"].(string), wake) {
			t.Fatal("live off gate woke the model")
		}
	}

	model.add("CLI_ROUTINE_CLEAR_NO_CONTEXT", mcpRuntimeTool("sh", map[string]string{"description": "Clear an effective precheck without requiring an approval call", "command": `XBOT_CALL_ID=missing "$XBOT_BIN" routine update ` + id + ` --self --args '{"precheck":""}' --json`}))
	cliSkillSend(t, srv, moved, "CLI_ROUTINE_CLEAR_NO_CONTEXT")
	cliRoutineDecode(t, model.results("CLI_ROUTINE_CLEAR_NO_CONTEXT")[0], &updated)
	if !updated.Applied || updated.ID != id || updated.Error != "" || readSaved().Precheck != "" {
		t.Fatal("explicit empty precheck wrongly required source approval context or failed to clear")
	}
	localCLI("machine", "set", "local", "allow")
	// Same-ID create resets the actual definition. Its precheck runs only when
	// Due/Deliver calls it, never during CLI save or update.
	nextAt := time.Now().Add(250 * time.Second).UTC()
	createJSON, _ = json.Marshal(map[string]any{"id": id, "name": "Allowed timer", "prompt": wake, "at": nextAt.Format(time.RFC3339), "precheck": "printf checked >> " + shellQuote(proof)})
	model.add("CLI_ROUTINE_LIVE_REARM", mcpRuntimeTool("sh", map[string]string{"description": "Rearm the same actual once routine after restoring live allow", "command": `"$XBOT_BIN" routine create --self --args ` + shellQuote(string(createJSON)) + ` --json`}))
	cliSkillSend(t, srv, moved, "CLI_ROUTINE_LIVE_REARM")
	cliRoutineDecode(t, model.results("CLI_ROUTINE_LIVE_REARM")[0], &created)
	if !created.Applied || created.ID != id {
		t.Fatal("same-ID create did not rearm the actual definition")
	}
	if _, err := os.Stat(proof); !os.IsNotExist(err) {
		t.Fatal("CRUD executed precheck instead of only reviewing it")
	}
	srv.Advance(70 * time.Second)
	WaitFor(t, 10*time.Second, "actual Timer delivers one event to moved Inbox", func() (bool, string) {
		count := 0
		for _, event := range mcpRuntimeInbox(t, srv, moved) {
			text, _ := event["content"].(string)
			if event["source"] == "timer" && event["type"] == "timer" && strings.Contains(text, wake) {
				count++
			}
		}
		return count == 1, "timer event not yet uniquely delivered"
	})
	WaitFor(t, 10*time.Second, "allowed once slot actually commits", func() (bool, string) {
		routine := readSaved()
		return routine.LastSlot != "" && !routine.Enabled, "allowed once slot not yet committed"
	})
	full, err := os.ReadFile(proof)
	if err != nil || string(full) != "checked" {
		t.Fatal("actual allowed delivery did not execute precheck exactly once")
	}
	srv.Advance(70 * time.Second)
	count := 0
	for _, event := range mcpRuntimeInbox(t, srv, moved) {
		text, _ := event["content"].(string)
		if event["source"] == "timer" && event["type"] == "timer" && strings.Contains(text, wake) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("actual Timer emitted %d duplicate events", count)
	}
	for _, path := range []string{filepath.Join(srv.Root, "bots", original), filepath.Join(srv.Root, "work", original)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("routine operation revived the original source directory")
		}
	}
}
