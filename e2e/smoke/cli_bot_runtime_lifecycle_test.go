//go:build smoke

package smoke

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// One cohesive creation-policy/real host deletion lifecycle. Permission cards,
// removed-after-key faults and their callback identities remain producer units.
func TestCLIBotRuntimeCreatePolicyAndHostDelete(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const peer, target, partial = "BotPolicyPeer", "BotLiveTarget", "BotPersonaPartial"
	srv.CreateBot(peer, "Actual authenticated policy peer.")
	readObject := func(path string) map[string]json.RawMessage {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]json.RawMessage
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	var primary struct{ Name string }
	primaryRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(primaryRaw, &primary); err != nil || primary.Name == "" {
		t.Fatal("fixture has no real Primary")
	}
	peerCfg := readObject(filepath.Join(srv.Root, "bots", peer, "config.json"))
	var owners []string
	if err := json.Unmarshal(peerCfg["owners"], &owners); err != nil || len(owners) == 0 {
		t.Fatal("actual API peer has no source owner")
	}
	owners = append(owners, "bot-inherited-other-owner")
	sourcePath := filepath.Join(srv.Root, "bots", primary.Name, "config.json")
	source := readObject(sourcePath)
	var existingOwners []string
	if len(source["owners"]) > 0 {
		if err := json.Unmarshal(source["owners"], &existingOwners); err != nil {
			t.Fatal(err)
		}
	}
	for _, owner := range existingOwners {
		if !slices.Contains(owners, owner) {
			owners = append(owners, owner)
		}
	}
	source["owners"], _ = json.Marshal(owners)
	source["role"] = json.RawMessage(`"dev"`)
	source["background_max_time"] = json.RawMessage(`1234`)
	updated, err := json.MarshalIndent(source, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, append(updated, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	// Give the source a real different registered binding. New must still use
	// the existing global default, not this source's provider/model/role/budget.
	localCLI := func(args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, xbotBinary, append([]string{"--root", srv.Root}, args...)...)
		cmd.Env = srv.env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("private setup CLI failed: %v; %.256q", err, out)
		}
	}
	localCLI("provider", "add", "bot-source-binding", "--base-url", model.server.URL+"/v1", "--api-key-env", "XBOT_API_KEY", "--model", "source-only-model", "--context", "1000000", "--hosted-web-search", "off")
	localCLI("bind", primary.Name, "--provider", "bot-source-binding", "--model", "source-only-model", "--uncertified")
	workspace := filepath.Join(srv.Root, "work", primary.Name)
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "explicit-duty.md"), []byte("  Host deletion lifecycle duty.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "oversized-duty.md"), []byte(strings.Repeat("p", (32<<10)+1)), 0600); err != nil {
		t.Fatal(err)
	}
	const createCue = "CLI_BOT_POLICY_CREATE"
	model.add(createCue, mcpRuntimeTool("sh", map[string]string{"description": "Create through the real Primary using a complete duty file", "command": `"$XBOT_BIN" new ` + target + ` --self --persona-file ./explicit-duty.md --json`}))
	cliSkillSend(t, srv, primary.Name, createCue)
	var created cliBotCreateReply
	cliBotDecode(t, model.results(createCue)[0], &created)
	if !created.Applied || created.Name != target || created.Stage != "complete" || created.Error != "" {
		t.Fatal("actual Primary could not create the lifecycle target")
	}
	var cfg struct {
		Provider, Model, Role, Persona string
		Owners, Links                  []string
		BackgroundMaxTime              int `json:"background_max_time"`
	}
	raw, err := os.ReadFile(filepath.Join(srv.Root, "bots", target, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	var sourceLinks []string
	if err := json.Unmarshal(source["links"], &sourceLinks); err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "deepseek" || cfg.Model != "smoke" || cfg.Role != "" || cfg.BackgroundMaxTime != 0 || cfg.Persona != "Host deletion lifecycle duty." || !slices.Equal(cfg.Owners, owners) || !slices.Equal(cfg.Links, sourceLinks) {
		t.Fatal("create inherited source binding/role/budget or lost complete owners/links")
	}

	const nonPrimary = "CLI_BOT_POLICY_NON_PRIMARY"
	model.add(nonPrimary,
		mcpRuntimeTool("sh", map[string]string{"description": "Reject creation by an ordinary source Bot", "command": `"$XBOT_BIN" new BotMustNotExist --self --persona denied --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Reject deletion by an ordinary source Bot", "command": `"$XBOT_BIN" delete ` + peer + ` --self --json`}),
	)
	cliSkillSend(t, srv, target, nonPrimary)
	if len(model.results(nonPrimary)) != 2 {
		t.Fatal("ordinary source did not execute both actual management refusal attempts")
	}
	for _, result := range model.results(nonPrimary) {
		if strings.HasPrefix(result, "exit 0\n") || strings.TrimSpace(commandOutput(result)) == "" {
			t.Fatal("ordinary source gained Primary management permissions")
		}
	}
	if _, err := os.Stat(filepath.Join(srv.Root, "bots", "BotMustNotExist")); !os.IsNotExist(err) {
		t.Fatal("ordinary source created a Bot")
	}
	if _, err := os.Stat(filepath.Join(srv.Root, "bots", peer, "config.json")); err != nil {
		t.Fatal("ordinary source removed an existing peer")
	}
	const refusals = "CLI_BOT_POLICY_REFUSALS"
	model.add(refusals,
		mcpRuntimeTool("sh", map[string]string{"description": "Reject empty duty before creating a definition", "command": `"$XBOT_BIN" new BotMissingDuty --self --persona ' ' --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Preserve the actual late persona write partial fact", "command": `"$XBOT_BIN" new ` + partial + ` --self --persona-file ./oversized-duty.md --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Preserve the real Primary deletion protection", "command": `"$XBOT_BIN" delete ` + shellQuote(primary.Name) + ` --self --json`}),
	)
	cliSkillSend(t, srv, primary.Name, refusals)
	results := model.results(refusals)
	for _, result := range results {
		if strings.HasPrefix(result, "exit 0\n") || strings.TrimSpace(commandOutput(result)) == "" {
			t.Fatal("empty/late-overflow/Primary protection unexpectedly succeeded")
		}
	}
	if _, err := os.Stat(filepath.Join(srv.Root, "bots", "BotMissingDuty")); !os.IsNotExist(err) {
		t.Fatal("empty-duty refusal created a Bot")
	}
	var part cliBotCreateReply
	if err := json.NewDecoder(strings.NewReader(commandOutput(results[1]))).Decode(&part); err != nil {
		t.Fatal("late persona error lost its actual JSON receipt")
	}
	if part.Name != partial || !part.Applied || part.Stage != "persona" || part.Error == "" {
		t.Fatal("late persona limit did not report true applied persona-stage partial")
	}
	for _, name := range []string{"config.json", "state.json"} {
		raw, err := os.ReadFile(filepath.Join(srv.Root, "bots", partial, name))
		if err != nil || !json.Valid(raw) {
			t.Fatal("late persona partial lost its actual committed config/state")
		}
	}
	if _, err := os.Stat(filepath.Join(srv.Root, "bots", partial, "persona.md")); !os.IsNotExist(err) {
		t.Fatal("oversized persona was silently written")
	}
	if _, err := os.Stat(filepath.Join(srv.Root, "bots", primary.Name, "config.json")); err != nil {
		t.Fatal("Primary protection failed")
	}

	// Start a real owned Local Shell job on the target's already warm entry.
	pidFile := filepath.Join(srv.Root, "owned-shell.pid")
	const hold = "CLI_BOT_POLICY_OWNED_JOB"
	model.add(hold, mcpRuntimeTool("sh", map[string]any{"description": "Keep one actual target-owned Shell job live before deletion", "run_in_background": true, "command": "printf '%s' \"$$\" > " + shellQuote(pidFile) + "; exec /bin/sleep 60"}))
	cliSkillSend(t, srv, target, hold)
	jobID := mcpRuntimeJobID(t, model, hold)
	WaitFor(t, 8*time.Second, "target owned process is actually running", func() (bool, string) {
		raw, err := os.ReadFile(pidFile)
		return err == nil && len(raw) > 0, "owned process not started"
	})
	pidRaw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(pidRaw))
	if err != nil {
		t.Fatal(err)
	}
	process, err := os.FindProcess(pid)
	if err != nil || process.Signal(syscall.Signal(0)) != nil {
		t.Fatal("owned process was not alive before deletion")
	}
	jobRaw, err := os.ReadFile(filepath.Join(srv.Root, "bots", target, "shell-jobs", jobID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var job struct{ Status string }
	if err := json.Unmarshal(jobRaw, &job); err != nil || job.Status != "running" {
		t.Fatal("actual target entry did not own a running job")
	}
	const remove = "CLI_BOT_POLICY_HOST_DELETE"
	model.add(remove, mcpRuntimeTool("sh", map[string]string{"description": "Delete the actual warm target entry and join its owned Local job", "command": `"$XBOT_BIN" delete ` + target + ` --self --json`}))
	cliSkillSend(t, srv, primary.Name, remove)
	var removed cliBotDeleteReply
	cliBotDecode(t, model.results(remove)[0], &removed)
	if !removed.Removed || removed.Name != target || removed.Stage != "complete" || removed.Error != "" {
		t.Fatal("warm host deletion did not report the actual removed fact")
	}
	if err := process.Signal(syscall.Signal(0)); err == nil {
		t.Fatal("host delete returned before the owned process stopped")
	}
	if _, err := os.Stat(filepath.Join(srv.Root, "bots", target)); !os.IsNotExist(err) {
		t.Fatal("owned completion revived the deleted target Bot directory")
	}
	const after = "CLI_BOT_POLICY_AFTER_DELETE"
	model.add(after, mcpRuntimeTool("sh", map[string]string{"description": "Read host status after owned completion and entry cleanup", "command": `"$XBOT_BIN" info --self --json`}))
	cliSkillSend(t, srv, primary.Name, after)
	var info cliBotInfoReply
	cliBotDecode(t, model.results(after)[0], &info)
	if info.Bots == nil || info.Error != "" {
		t.Fatal("actual host status unavailable after delete")
	}
	for _, status := range *info.Bots {
		if status.Bot == target {
			t.Fatal("deleted host entry remained discoverable")
		}
	}
}
