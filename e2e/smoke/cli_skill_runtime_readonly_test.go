//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// This observes actual main and real continuable research/dev requests. Legacy
// Dispatch=false requests remain the implementation's direct-provider coverage;
// do not invent a second serve entry to duplicate that unit boundary.
func TestCLISkillRuntimeReadOnlyAndSchemas(t *testing.T) {
	cliSkillIsolation(t)
	model := &mcpRuntimeModel{scripts: map[string]*mcpRuntimeScript{}}
	var mu sync.Mutex
	retired := false
	model.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad scripted input", 400)
			return
		}
		var request stubRequest
		if json.Unmarshal(raw, &request) != nil {
			http.Error(w, "bad scripted JSON", 400)
			return
		}
		caller, _ := classify(request)
		if caller != callerCompaction && caller != callerExtract {
			mu.Lock()
			retired = retired || bytes.Contains(raw, []byte("read_skill")) || bytes.Contains(raw, []byte("write_skill"))
			mu.Unlock()
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		model.handle(w, r)
	}))
	t.Cleanup(model.server.Close)
	srv := Start(t, &Stub{srv: model.server})
	srv.CreateBot("SkillGateWorker", "Independent skills readonly and retired-door acceptance.")
	srv.CreateBot("SkillGatePeer", "Existing native bot directory tool acceptance.")
	workspace := filepath.Join(srv.Root, "work", "SkillGateWorker")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "other-tool-proof.txt"), []byte("OTHER_NATIVE_TOOL_STILL_WORKS"), 0600); err != nil {
		t.Fatal(err)
	}
	const mainCue, research, dev = "CLI_SKILL_GATE_MAIN", "CLI_SKILL_GATE_RESEARCH", "CLI_SKILL_GATE_DEV"
	read := `"$XBOT_BIN" skill read gate-skill --self --json`
	injection := filepath.Join(workspace, "injection-must-not-run")
	model.add(research,
		mcpRuntimeTool("sh", map[string]string{"description": "Read a skill in research", "command": read}),
		mcpRuntimeTool("sh", map[string]string{"description": "Read skills CLI help in research", "command": `"$XBOT_BIN" skill read --help`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject skill writes in research", "command": `"$XBOT_BIN" skill write research-must-not-write --description denied --body denied --self --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject shell operators in a claimed readonly read", "command": read + `; printf injected > ` + shellQuote(injection)}),
	)
	model.add(dev,
		mcpRuntimeTool("sh", map[string]string{"description": "Read a skill through a real dev source", "command": read}),
		mcpRuntimeTool("read_file", map[string]string{"path": "other-tool-proof.txt"}),
	)
	model.add(mainCue,
		mcpRuntimeTool("links", map[string]string{"action": "load", "id": "skills"}),
		mcpRuntimeTool("sh", map[string]string{"description": "Prepare a skill for actual subagent reads", "command": `"$XBOT_BIN" skill write gate-skill --description 'Readonly fixture' --body READONLY_SKILL_BODY --self --json`}),
		mcpRuntimeTool("tools", map[string]any{"action": "call", "id": "list_bots", "args": map[string]any{}}),
		mcpRuntimeTool("dispatch", map[string]string{"type": "research", "task": research}),
		mcpRuntimeTool("dispatch", map[string]string{"type": "dev", "task": dev}),
	)
	cliSkillSend(t, srv, "SkillGateWorker", mainCue)
	for _, task := range []string{research, dev} {
		WaitFor(t, 12*time.Second, "actual skills child steps", func() (bool, string) {
			results := model.results(task)
			return results[len(results)-1] != "", "child steps not completed"
		})
	}
	mainResults := model.results(mainCue)
	if strings.Contains(mainResults[0], "read_skill") || strings.Contains(mainResults[0], "write_skill") {
		t.Fatal("skills load still returns retired model tools")
	}
	var written cliSkillWriteReply
	cliSkillDecode(t, mainResults[1], &written)
	if !written.Written {
		t.Fatal("gate fixture was not actually written")
	}
	if strings.HasPrefix(mainResults[2], "error:") || !strings.Contains(mainResults[2], "SkillGatePeer") {
		t.Fatal("the other native Tools door stopped executing list_bots")
	}
	r := model.results(research)
	var readReply cliSkillReadReply
	cliSkillDecode(t, r[0], &readReply)
	if readReply.Body == nil || *readReply.Body != "READONLY_SKILL_BODY" {
		t.Fatal("research read lost the complete actual body")
	}
	if !strings.HasPrefix(r[1], "exit 0\n") || !strings.Contains(strings.ToLower(r[1]), "skill") {
		t.Fatal("research CLI help was not allowed")
	}
	if strings.HasPrefix(r[2], "exit 0\n") || strings.HasPrefix(r[3], "exit 0\n") {
		t.Fatal("research allowed a write or shell-operator command")
	}
	if _, err := os.Stat(filepath.Join(srv.Root, "skills", "research-must-not-write")); !os.IsNotExist(err) {
		t.Fatal("rejected research write created a skill")
	}
	if _, err := os.Stat(injection); !os.IsNotExist(err) {
		t.Fatal("rejected pseudo-read executed its shell operator")
	}
	d := model.results(dev)
	cliSkillDecode(t, d[0], &readReply)
	if readReply.Body == nil || *readReply.Body != "READONLY_SKILL_BODY" || !strings.Contains(d[1], "OTHER_NATIVE_TOOL_STILL_WORKS") {
		t.Fatal("real dev source read or the other native file tool regressed")
	}
	model.mu.Lock()
	mainSeen := model.scripts[mainCue].callers[callerForeground]
	researchSeen := model.scripts[research].callers[callerSubagent]
	devSeen := model.scripts[dev].callers[callerSubagent]
	model.mu.Unlock()
	if !mainSeen || !researchSeen || !devSeen {
		t.Fatal("schema checks did not reach real main/research/dev provider requests")
	}
	mu.Lock()
	old := retired
	mu.Unlock()
	if old {
		t.Fatal("actual provider requests still expose a retired skill schema, index or instruction")
	}
}
