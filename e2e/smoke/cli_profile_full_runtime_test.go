//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Independent full-profile increment. Only the provider is scripted; the
// binary, source Shell, bound endpoint and rename are the actual product.
// Keep the accepted ordinary profile/avatar/MCP files unchanged.
func TestCLIProfileFullRuntimeRenameEntry(t *testing.T) {
	cliProfileIsolation(t)
	model := newCLIProfileModel(t)
	const cue = "FULL_PROFILE_RENAME_ENTRY"
	model.add(cue, -1, `"$XBOT_BIN" profile set --self --name FullProfileRenamed --json`)
	srv := Start(t, &Stub{srv: model.server})
	srv.CreateBot("FullProfileWorker", "Independent full runtime profile rename acceptance.")
	srv.Send("FullProfileWorker", cue)
	value := cliProfileJSON(t, cliProfileResult(t, model, cue, 0))
	if value["name"] != "FullProfileRenamed" {
		t.Fatalf("self rename did not return the actual new name: %v", value)
	}
	status, _ := srv.do(http.MethodGet, "/api/bots/FullProfileRenamed", nil)
	if status != http.StatusOK {
		t.Fatalf("real renamed Bot unavailable through the existing API: HTTP %d", status)
	}
}

func TestCLIProfileFullRuntimeAtomic(t *testing.T) {
	cliProfileIsolation(t)
	peer := newMCPRuntimePeer(t)
	model := newCLIProfileFullModel(t, nil)
	model.base.add("FULL_ATOMIC_WARM", mcpRuntimeCall("counter", "full-before")...)
	srv := Start(t, &Stub{srv: model.server})
	srv.CreateBot("Worker", "Independent combined runtime profile acceptance.")
	srv.CreateBot("Taken", "A real target-name collision.")
	srv.PatchBot("Worker", map[string]any{"description": "before full profile", "timezone": "UTC", "avatar_seed": "Worker#original", "persona": "FULL_PERSONA_BEFORE"})
	cliProfileMustRun(t, srv.env, "--root", srv.Root, "profile", "avatar", "Worker", "--file", cliAvatarPNG(t))
	status, _ := cliFullAPI(t, srv, http.MethodPost, "/api/bots/Worker/mcp", map[string]any{"name": "runtime-fixture", "url": peer.server.URL + "/full-atomic"})
	if status != http.StatusOK {
		t.Fatalf("install actual MCP fixture: HTTP %d", status)
	}
	cliFullSend(t, srv, "Worker", "FULL_ATOMIC_WARM")
	beforeCall := mcpRuntimeResult(t, model.base, "FULL_ATOMIC_WARM")
	before := cliFullSnapshot(t, srv.Root, "Worker")
	beforeConfig := cliProfileDisk(t, srv.Root, "Worker")
	blockedWork := filepath.Join(srv.Root, "work", "BlockedWork")
	if err := os.MkdirAll(blockedWork, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blockedWork, "collision.txt"), []byte("keep target workspace"), 0600); err != nil {
		t.Fatal(err)
	}
	keyDir := filepath.Join(srv.Root, "keys", "vault")
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		t.Fatal(err)
	}
	// Empty collision sentinels contain no credential and are never opened as
	// a vault. The preflight must reject them before profile fields are saved.
	for _, name := range []string{"Worker", "BlockedKey"} {
		file, err := os.OpenFile(filepath.Join(keyDir, name+".key"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		file.Close()
	}
	for i, tc := range []struct{ name, timezone string }{
		{"Taken", "Asia/Tokyo"}, {"BlockedWork", "Asia/Tokyo"}, {"BlockedKey", "Asia/Tokyo"}, {"WouldRename", "Invalid/Nowhere"},
	} {
		cue := fmt.Sprintf("FULL_ATOMIC_REJECT_%d", i)
		model.base.add(cue, mcpRuntimeShell(`"$XBOT_BIN" profile set --self --name `+shellQuote(tc.name)+` --description rejected --timezone `+shellQuote(tc.timezone)+` --avatar-seed rejected --json`))
		notices := cliFullSend(t, srv, "Worker", cue)
		result := model.base.results(cue)[0]
		if !strings.HasPrefix(result, "exit 1\n") {
			t.Fatalf("predictable combined conflict did not reject: %s", result)
		}
		if len(notices) != 0 {
			t.Fatal("predictable rejection published a false profile change")
		}
		cliFullUnchanged(t, srv.Root, "Worker", before)
	}
	status, _ = cliFullAPI(t, srv, http.MethodPatch, "/api/bots/Worker", map[string]any{"name": "Taken", "description": "web rejected", "timezone": "Asia/Tokyo", "avatar_seed": "web rejected", "persona": "FULL_PERSONA_MUST_NOT_APPLY"})
	if status < 400 {
		t.Fatalf("web combination accepted a predictable target conflict: HTTP %d", status)
	}
	cliFullUnchanged(t, srv.Root, "Worker", before)

	// One actual Shell invocation retains the literal original environment
	// through combined rename, a second profile CLI and an MCP CLI call.
	const cue = "FULL_ATOMIC_COMMIT"
	command := `printf 'FULL_ENDPOINT_BEFORE=%s\n' "$XBOT_ENDPOINT"; "$XBOT_BIN" profile set --self --name FullRenamed --description 'combined runtime saved' --timezone Asia/Tokyo --avatar-seed 'FullRenamed#combined' --json && "$XBOT_BIN" profile show --self --json && "$XBOT_BIN" mcp call runtime-fixture counter --args '{"label":"full-after"}' --self --json && printf 'FULL_ENDPOINT_AFTER=%s\n' "$XBOT_ENDPOINT"`
	model.base.add(cue, mcpRuntimeShell(command), mcpRuntimeShell(`printf 'full-next-step\n' >> full-next.txt; pwd -P`), mcpRuntimeShell(`"$XBOT_BIN" persona set --self --text FULL_PERSONA_RETAINED_TOOL --json`))
	notices := cliFullSend(t, srv, "Worker", cue)
	results := model.base.results(cue)
	if !strings.HasPrefix(results[0], "exit 0\n") {
		t.Fatalf("combined source script failed: %s", results[0])
	}
	var lines [][]byte
	var endpoints []string
	for _, line := range strings.Split(strings.TrimSpace(commandOutput(results[0])), "\n") {
		if strings.HasPrefix(line, "{") {
			lines = append(lines, []byte(line))
		}
		if strings.HasPrefix(line, "FULL_ENDPOINT_") {
			endpoints = append(endpoints, strings.SplitN(line, "=", 2)[1])
		}
	}
	if len(lines) != 3 || len(endpoints) != 2 || endpoints[0] == "" || endpoints[0] != endpoints[1] {
		t.Fatal("original Shell environment/endpoint or three complete CLI replies were lost")
	}
	value := cliFullValue(t, lines[0])
	if value.Name != "FullRenamed" || value.PreviousName != "Worker" || !value.Moved || !value.Applied || value.Error != "" {
		t.Fatalf("combined rename receipt did not describe the actual move: %+v", value)
	}
	for _, field := range []string{"name", "description", "timezone", "avatar_seed"} {
		found := false
		for _, applied := range value.AppliedFields {
			found = found || applied == field
		}
		if !found {
			t.Fatalf("completed combined receipt missing %s: %+v", field, value)
		}
	}
	for _, raw := range lines[:2] {
		got := cliFullValue(t, raw)
		if got.Name != "FullRenamed" || got.Description != "combined runtime saved" || got.Timezone != "Asia/Tokyo" || got.Avatar != "generated" || got.AvatarSeed != "FullRenamed#combined" || got.Scope != "runtime" {
			t.Fatalf("combined profile or original-endpoint show stale: %+v", got)
		}
	}
	afterCall := mcpRuntimeCounter(t, mcpRuntimeJSON(t, string(lines[2])))
	if beforeCall.Count != 1 || afterCall.Count != 2 || beforeCall.Session != afterCall.Session {
		t.Fatalf("rename changed the original MCP ref/session: before=%+v after=%+v", beforeCall, afterCall)
	}
	foundNotice := false
	for _, notice := range notices {
		foundNotice = foundNotice || notice.Name == "FullRenamed" && notice.PreviousName == "Worker"
	}
	if !foundNotice {
		t.Fatal("original SSE omitted actual previous_name rename notice")
	}
	if !strings.Contains(results[1], filepath.Join(srv.Root, "work", "FullRenamed")) || string(cliFullRead(t, filepath.Join(srv.Root, "work", "FullRenamed", "full-next.txt"))) != "full-next-step\n" {
		t.Fatal("the next actual Shell step wrote or resolved its cwd under the old name")
	}
	// ADR0054 retires native persona; keep this post-rename independent duty
	// business on its current source CLI without rerunning the profile family.
	var persona struct {
		Name             string
		Written          bool
		Effective, Error string
	}
	cliBotDecode(t, results[2], &persona)
	if persona.Name != "FullRenamed" || !persona.Written || persona.Effective != "next_wake" || persona.Error != "" || !bytes.Contains(cliFullRead(t, filepath.Join(srv.Root, "bots", "FullRenamed", "persona.md")), []byte("FULL_PERSONA_RETAINED_TOOL")) {
		t.Fatal("current source persona CLI lost the independent post-rename duty business")
	}
	afterConfig := cliProfileDisk(t, srv.Root, "FullRenamed")
	for _, field := range []string{"provider", "model", "role", "skills", "card"} {
		a, _ := json.Marshal(beforeConfig[field])
		b, _ := json.Marshal(afterConfig[field])
		if !bytes.Equal(a, b) {
			t.Fatalf("combined update changed unrequested %s", field)
		}
	}
	for _, file := range []string{"avatar", "avatar.mime"} {
		if _, err := os.Stat(filepath.Join(srv.Root, "bots", "FullRenamed", file)); !os.IsNotExist(err) {
			t.Fatalf("completed seed retained moved upload %s: %v", file, err)
		}
	}
	cliFullAbsent(t, srv.Root, "Worker")
	model.base.add("FULL_SAME_NAME", mcpRuntimeShell(`"$XBOT_BIN" profile set --self --name FullRenamed --json`))
	cliFullSend(t, srv, "FullRenamed", "FULL_SAME_NAME")
	if got := cliFullShell(t, model.base.results("FULL_SAME_NAME")[0]); got.Name != "FullRenamed" || got.Moved {
		t.Fatalf("same-name request pretended a physical move: %+v", got)
	}
	model.assertRetired(t)
}

func TestCLIProfileFullRuntimeResources(t *testing.T) {
	cliProfileIsolation(t)
	const task = "FULL_LIVE_LOCAL"
	entered, released, steered := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var enterOnce, releaseOnce, steerOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(released) }) }
	defer release()
	var childMu sync.Mutex
	childResults := map[string]string{}
	model := newCLIProfileFullModel(t, func(w http.ResponseWriter, r *http.Request, req continuationRequest) {
		if req.Model != "child-original" || !req.text("user", task) {
			t.Error("active/cold original child lost its selected model or task")
			http.Error(w, "wrong child identity", 400)
			return
		}
		if req.text("user", "FULL_LOCAL_STEER") {
			steerOnce.Do(func() { close(steered) })
		}
		if req.text("user", "FULL_LOCAL_COLD") {
			if !req.text("assistant", "FULL_LOCAL_FINISHED") {
				t.Error("cold original child lost its completed first conclusion")
			}
			if result, ok := req.result("full_cold_path"); ok {
				childMu.Lock()
				childResults["cold"] = result
				childMu.Unlock()
				streamText(w, []string{"FULL_LOCAL_COLD_FINISHED"})
				return
			}
			streamTool(w, "full_cold_path", "sh", argsJSON(map[string]string{"description": "Read the same moved workspace in the original cold child", "command": `pwd -P; cat full-path-proof.txt; "$XBOT_BIN" profile show --self --json`}))
			return
		}
		before, ok := req.result("full_path_before")
		if !ok {
			streamTool(w, "full_path_before", "sh", argsJSON(map[string]string{"description": "Record a workspace side effect before the live rename", "command": `mkdir -p full-project && cd full-project && pwd -P && printf 'before\n' >> full-path-proof.txt`}))
			return
		}
		childMu.Lock()
		childResults["before"] = before
		childMu.Unlock()
		if result, ok := req.result("full_path_after"); !ok {
			enterOnce.Do(func() { close(entered) })
			select {
			case <-released:
			case <-r.Context().Done():
				return
			}
			streamTool(w, "full_path_after", "sh", argsJSON(map[string]string{"description": "Continue in the moved workspace through the original source endpoint", "command": `pwd -P; printf 'after\n' >> full-path-proof.txt; "$XBOT_BIN" profile show --self --json`}))
			return
		} else {
			childMu.Lock()
			childResults["after"] = result
			childMu.Unlock()
		}
		if _, ok := req.result("full_parent_message"); !ok {
			streamTool(w, "full_parent_message", "send_message_to_parent", `{"text":"FULL_PARENT_AFTER_RENAME"}`)
			return
		}
		streamText(w, []string{"FULL_LOCAL_FINISHED"})
	})
	peer := newMCPRuntimePeer(t)
	jobGate := peer.gate("full-original-job", 0)
	defer jobGate.allow()
	srv := Start(t, &Stub{srv: model.server})
	srv.CreateBot("Worker", "Independent original resources across full runtime rename.")
	cliProfileMustRun(t, srv.env, "--root", srv.Root, "provider", "add", "full-child", "--base-url", model.server.URL+"/v1", "--api-key-env", "XBOT_API_KEY", "--model", "child-original", "--context", "1000000", "--hosted-web-search", "off")
	at := time.Now().Add(time.Minute).UTC().Format(time.RFC3339)
	model.base.add("FULL_RESOURCE_START",
		mcpRuntimeTool("tools", map[string]any{"action": "call", "id": "routine_create", "args": map[string]string{"id": "full-timer", "name": "before move", "prompt": "FULL_TIMER_WAKE", "at": at}}),
		mcpRuntimeTool("sh", map[string]any{"description": "Hold one actual original Shell job across move", "run_in_background": true, "command": `curl --fail --silent ` + shellQuote(peer.server.URL+"/gate/full-original-job") + ` && pwd -P && "$XBOT_BIN" profile show --self --json`}),
		mcpRuntimeTool("dispatch", map[string]string{"type": "dev", "task": task, "provider": "full-child", "model": "child-original"}),
	)
	cliFullSend(t, srv, "Worker", "FULL_RESOURCE_START")
	mcpRuntimeWait(t, entered, 10*time.Second, "real Local did not reach the pre-rename model gate")
	mcpRuntimeWait(t, jobGate.entered, 10*time.Second, "original Shell job did not reach its real HTTP gate")
	jobID := mcpRuntimeJobID(t, model.base, "FULL_RESOURCE_START")
	status, raw := cliFullAPI(t, srv, http.MethodGet, "/api/bots/Worker", nil)
	var panel struct{ Agents []Agent }
	if status != 200 || json.Unmarshal(raw, &panel) != nil || len(panel.Agents) != 1 {
		t.Fatal("real dispatch did not expose exactly one active Local")
	}
	id := panel.Agents[0].ID
	if panel.Agents[0].Task != task || panel.Agents[0].Status != "running" {
		t.Fatalf("wrong original active child: %+v", panel.Agents[0])
	}
	oldChildDir := filepath.Join(srv.Root, "bots", "Worker", "agents", id)
	oldRows := cliFullRead(t, filepath.Join(oldChildDir, "transcript.jsonl"))
	model.base.add("FULL_RESOURCE_RENAME", mcpRuntimeShell(`"$XBOT_BIN" profile set --self --name MovedWorker --description 'resources moved' --timezone Asia/Tokyo --avatar-seed 'MovedWorker#resources' --json && "$XBOT_BIN" profile show --self --json`))
	cliFullSend(t, srv, "Worker", "FULL_RESOURCE_RENAME")
	renameResult := model.base.results("FULL_RESOURCE_RENAME")[0]
	if !strings.HasPrefix(renameResult, "exit 0\n") {
		t.Fatalf("allowed live Local/Job rename failed or introduced busy rejection: %s", renameResult)
	}
	lines := strings.Split(strings.TrimSpace(commandOutput(renameResult)), "\n")
	if len(lines) != 2 {
		t.Fatal("original endpoint did not finish rename plus second CLI")
	}
	for _, line := range lines {
		if value := cliFullValue(t, []byte(line)); value.Name != "MovedWorker" || value.Scope != "runtime" {
			t.Fatalf("same script resolved old route: %+v", value)
		}
	}
	newChildDir := filepath.Join(srv.Root, "bots", "MovedWorker", "agents", id)
	if !bytes.HasPrefix(cliFullRead(t, filepath.Join(newChildDir, "transcript.jsonl")), oldRows) {
		t.Fatal("live move rewrote old child transcript rows")
	}
	if got := cliFullJob(t, srv, "MovedWorker", jobID); got["id"] != jobID || got["status"] != "running" {
		t.Fatalf("move replaced/lost original running Job: %v", got)
	}
	// Before releasing either task, a real new-name web turn must acquire the
	// same host entry and use the Timer captured before the move.
	model.base.add("FULL_AFTER_MOVE", mcpRuntimeTool("tools", map[string]any{"action": "call", "id": "routine_update", "args": map[string]string{"id": "full-timer", "name": "after move"}}), mcpRuntimeTool("tools", map[string]any{"action": "call", "id": "routine_list", "args": map[string]any{}}), mcpRuntimeTool("shell_status", map[string]string{"id": jobID}))
	cliFullSend(t, srv, "MovedWorker", "FULL_AFTER_MOVE")
	post := model.base.results("FULL_AFTER_MOVE")
	if !strings.Contains(post[0], "updated") || !strings.Contains(post[1], "after move") || !strings.Contains(post[2], jobID+" running") {
		t.Fatalf("new-name hold/old Timer/original Job stopped working: %v", post)
	}
	cliFullAbsent(t, srv.Root, "Worker")
	status, raw = cliFullAPI(t, srv, http.MethodPost, "/api/bots/MovedWorker/agents/"+id+"/messages", map[string]string{"text": "FULL_LOCAL_STEER", "delivery": "steer"})
	var receipt continuationAgent
	if status != 200 || json.Unmarshal(raw, &receipt) != nil || receipt.ID != id {
		t.Fatalf("live original Local could not be steered through its moved route: HTTP %d", status)
	}
	model.base.add("FULL_TIMER_WAKE")
	srv.Advance(70 * time.Second)
	cliFullOneInbox(t, srv, "MovedWorker", "timer", "timer", "", "FULL_TIMER_WAKE")
	release()
	jobGate.allow()
	mcpRuntimeWait(t, steered, 10*time.Second, "accepted steer was not consumed at the next real original-child step")
	WaitFor(t, 12*time.Second, "original Local first completion in moved directory", func() (bool, string) {
		a := cliFullAgent(t, srv, "MovedWorker", id)
		return a.Status == "done", a.Status
	})
	first := cliFullAgent(t, srv, "MovedWorker", id)
	if !first.Continuable || !strings.Contains(first.ResultPath, newChildDir+string(os.PathSeparator)) {
		t.Fatalf("first original child lacks current immutable result: %+v", first)
	}
	firstResult := cliFullRead(t, first.ResultPath)
	if !bytes.Contains(firstResult, []byte("FULL_LOCAL_FINISHED")) {
		t.Fatal("first real Local result lacks its final conclusion")
	}
	cliFullOneInbox(t, srv, "MovedWorker", "subagent", "message", id, "FULL_PARENT_AFTER_RENAME")
	cliFullOneInbox(t, srv, "MovedWorker", "subagent", "done", id, first.ResultPath)
	WaitFor(t, 10*time.Second, "original Shell completion at moved path", func() (bool, string) {
		job := cliFullJob(t, srv, "MovedWorker", jobID)
		return job["status"] == "done", fmt.Sprint(job["status"])
	})
	job := cliFullJob(t, srv, "MovedWorker", jobID)
	if exit, _ := job["exit_code"].(float64); exit != 0 {
		t.Fatalf("original moved Shell failed: %v", job)
	}
	output := string(cliFullRead(t, fmt.Sprint(job["output"])))
	if !strings.Contains(output, `"name":"MovedWorker"`) || !strings.Contains(output, filepath.Join(srv.Root, "work", "MovedWorker")) {
		t.Fatal("original running Shell lost current profile endpoint or physical cwd")
	}
	cliFullOneInbox(t, srv, "MovedWorker", "shell", "shell_job", jobID, jobID)
	childMu.Lock()
	after := childResults["after"]
	childMu.Unlock()
	if !strings.HasPrefix(after, "exit 0\n") || !strings.Contains(after, `"name":"MovedWorker"`) || !strings.Contains(after, filepath.Join(srv.Root, "work", "MovedWorker", "full-project")) {
		t.Fatalf("live Local continued with stale cwd/profile route: %s", after)
	}
	proof := filepath.Join(srv.Root, "work", "MovedWorker", "full-project", "full-path-proof.txt")
	if string(cliFullRead(t, proof)) != "before\nafter\n" {
		t.Fatal("live move replayed a side effect or wrote outside the moved workspace")
	}
	rowsBeforeCold := cliFullRead(t, filepath.Join(newChildDir, "transcript.jsonl"))
	cliFullAbsent(t, srv.Root, "Worker")
	// Restart, then rename before Restore: the empty Local must recover the
	// original selected model and project subdirectory from moved disk records.
	srv.Restart()
	model.base.add("FULL_COLD_RENAME", mcpRuntimeShell(`"$XBOT_BIN" profile set --self --name ColdWorker --json`))
	cliFullSend(t, srv, "MovedWorker", "FULL_COLD_RENAME")
	if value := cliFullShell(t, model.base.results("FULL_COLD_RENAME")[0]); value.Name != "ColdWorker" || !value.Moved {
		t.Fatalf("pre-Restore rename did not move current route: %+v", value)
	}
	coldChildDir := filepath.Join(srv.Root, "bots", "ColdWorker", "agents", id)
	movedFirstResult := filepath.Join(coldChildDir, "results", filepath.Base(first.ResultPath))
	status, raw = cliFullAPI(t, srv, http.MethodPost, "/api/bots/ColdWorker/agents/"+id+"/messages", map[string]string{"text": "FULL_LOCAL_COLD"})
	if status != 200 || json.Unmarshal(raw, &receipt) != nil || receipt.ID != id {
		t.Fatalf("cold original child was not accepted by current name: HTTP %d", status)
	}
	WaitFor(t, 12*time.Second, "cold original ID second turn", func() (bool, string) {
		a := cliFullAgent(t, srv, "ColdWorker", id)
		return a.Status == "done" && a.TurnSeq > first.TurnSeq, fmt.Sprintf("status=%s turn=%d", a.Status, a.TurnSeq)
	})
	second := cliFullAgent(t, srv, "ColdWorker", id)
	childMu.Lock()
	cold := childResults["cold"]
	childMu.Unlock()
	if !strings.HasPrefix(cold, "exit 0\n") || !strings.Contains(cold, `"name":"ColdWorker"`) || !strings.Contains(cold, filepath.Join(srv.Root, "work", "ColdWorker", "full-project")) || !strings.Contains(cold, "before\nafter\n") {
		t.Fatalf("cold same-ID continuation lost its current route/history/workspace: %s", cold)
	}
	if !strings.Contains(second.ResultPath, coldChildDir+string(os.PathSeparator)) || second.ResultPath == movedFirstResult || !bytes.Equal(cliFullRead(t, movedFirstResult), firstResult) {
		t.Fatal("cold turn lost new-path result identity or rewrote first immutable result")
	}
	if !bytes.HasPrefix(cliFullRead(t, filepath.Join(coldChildDir, "transcript.jsonl")), rowsBeforeCold) {
		t.Fatal("cold resume rewrote original child rows")
	}
	cliFullAbsent(t, srv.Root, "Worker")
	cliFullAbsent(t, srv.Root, "MovedWorker")
	model.assertRetired(t)
}
