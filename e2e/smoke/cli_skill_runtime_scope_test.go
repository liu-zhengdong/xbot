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
)

// One scope lifecycle through the actual server, native Shell and current BIN.
// Fixture ownership changes precede source creation; no domain handler is called
// directly and no provider other than the scripted upstream model is involved.
func TestCLISkillRuntimeScopeLifecycle(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const worker, moved, slug = "ScopeWorker", "ScopeMoved", "runtime-scope"
	for _, name := range []string{worker, "BoB", "global", "OtherOwner", "Shared", "Orphan"} {
		srv.CreateBot(name, "Independent skill scope lifecycle acceptance.")
	}
	configPaths := func() []string {
		t.Helper()
		paths, err := filepath.Glob(filepath.Join(srv.Root, "bots", "*", "config.json"))
		if err != nil {
			t.Fatal(err)
		}
		return paths
	}
	var localNames []string
	var owner string
	workerRaw, err := os.ReadFile(filepath.Join(srv.Root, "bots", worker, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var workerConfig struct{ Owners []string }
	if err := json.Unmarshal(workerRaw, &workerConfig); err != nil {
		t.Fatal(err)
	}
	if len(workerConfig.Owners) != 1 {
		t.Fatal("source fixture needs its actual authenticated owner")
	}
	owner = workerConfig.Owners[0]
	for _, path := range configPaths() {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var cfg map[string]json.RawMessage
		if err := json.Unmarshal(raw, &cfg); err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(filepath.Dir(path))
		localNames = append(localNames, name)
		// Preserve this run's real owner for the source/peer. The other three
		// controlled on-disk fixtures exercise all-local enumeration, without
		// emitting configuration or identity contents in failure diagnostics.
		switch name {
		case "OtherOwner":
			cfg["owners"], _ = json.Marshal([]string{"scope-other-owner"})
		case "Shared":
			cfg["owners"], _ = json.Marshal([]string{owner, "scope-other-owner"})
		case "Orphan":
			cfg["owners"] = json.RawMessage(`[]`)
		}
		cfg["disabled_skills"], _ = json.Marshal([]string{"scope-unrelated", slug})
		updated, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(updated, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}

	assertSwitches := func(enabled ...string) {
		t.Helper()
		for _, name := range localNames {
			raw, err := os.ReadFile(filepath.Join(srv.Root, "bots", name, "config.json"))
			if err != nil {
				t.Fatal(err)
			}
			var cfg struct {
				Disabled []string `json:"disabled_skills"`
			}
			if err := json.Unmarshal(raw, &cfg); err != nil {
				t.Fatal(err)
			}
			if slices.Contains(cfg.Disabled, slug) == slices.Contains(enabled, name) || !slices.Contains(cfg.Disabled, "scope-unrelated") {
				t.Fatalf("scope switches incorrect for local Bot %s", name)
			}
		}
	}
	writeTurn := func(cue, caller, command, target string) {
		t.Helper()
		model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Apply skill scope through the original runtime source", "command": command}))
		cliSkillSend(t, srv, caller, cue)
		var reply cliSkillWriteReply
		cliSkillDecode(t, model.results(cue)[0], &reply)
		if !reply.Written || reply.Slug != slug || reply.Error != "" || target != "" && reply.Target != target {
			t.Fatal("runtime scope write did not report the actual write and resolved target")
		}
		for _, name := range localNames {
			if !slices.Contains(reply.UpdatedBots, name) {
				t.Fatalf("scope write receipt omitted updated local Bot %s", name)
			}
		}
	}
	readPeer := func(cue, want string) {
		t.Helper()
		model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Observe the skill from the already-running peer source", "command": `"$XBOT_BIN" skill read ` + slug + ` --self --json`}))
		cliSkillSend(t, srv, "BoB", cue)
		var reply cliSkillReadReply
		cliSkillDecode(t, model.results(cue)[0], &reply)
		if reply.Slug != slug || reply.Body == nil || *reply.Body != want || reply.Error != "" {
			t.Fatal("running peer did not observe the current skill body and manual switch")
		}
	}

	// Seeded disabled switches make the omitted-scope assertion meaningful for
	// different-owner/shared/orphan fixtures, beyond the first entry's two Bots.
	writeTurn("CLI_SKILL_SCOPE_ALL", worker, `"$XBOT_BIN" skill write `+slug+` --description 'Scope lifecycle' --body ALL_LOCAL_BODY --self --json`, "")
	assertSwitches(localNames...)
	writeTurn("CLI_SKILL_SCOPE_REAL_GLOBAL", worker, `"$XBOT_BIN" skill write `+slug+` --description 'Scope lifecycle' --body GLOBAL_ONLY_BODY --scope global --self --json`, "global")
	assertSwitches("global")
	writeTurn("CLI_SKILL_SCOPE_CASEFOLD", worker, `"$XBOT_BIN" skill write `+slug+` --description 'Scope lifecycle' --body BOB_ONLY_BODY --scope bOb --self --json`, "BoB")
	assertSwitches("BoB")
	readPeer("CLI_SKILL_SCOPE_WARM_PEER", "BOB_ONLY_BODY")

	// Both commands inherit the original endpoint in one real Shell. Renaming
	// rebinds its Resources; the deliberately stale diagnostic points at BoB.
	model.add("CLI_SKILL_SCOPE_RENAME_SELF", mcpRuntimeTool("sh", map[string]string{"description": "Write self scope through the original endpoint after rename", "command": `"$XBOT_BIN" profile set --name ` + moved + ` --self --json > scope-rename.json && XBOT_BOT=BoB "$XBOT_BIN" skill write ` + slug + ` --description 'Scope lifecycle' --body CURRENT_SELF_BODY --scope self --self --json`}))
	cliSkillSend(t, srv, worker, "CLI_SKILL_SCOPE_RENAME_SELF")
	var self cliSkillWriteReply
	cliSkillDecode(t, model.results("CLI_SKILL_SCOPE_RENAME_SELF")[0], &self)
	if !self.Written || self.Slug != slug || self.Target != moved || self.Error != "" {
		t.Fatal("scope self did not resolve the renamed current Bot from the original source")
	}
	localNames[slices.Index(localNames, worker)] = moved
	for _, name := range localNames {
		if !slices.Contains(self.UpdatedBots, name) {
			t.Fatalf("self write receipt omitted updated local Bot %s", name)
		}
	}
	assertSwitches(moved)

	const enableCue = "CLI_SKILL_SCOPE_MANUAL_ENABLE"
	model.add(enableCue, mcpRuntimeTool("sh", map[string]string{"description": "Manually enable the skill with the preserved offline CLI", "command": `"$XBOT_BIN" --root "$XBOT_ROOT" skill enable BoB ` + slug}))
	cliSkillSend(t, srv, moved, enableCue)
	if !strings.HasPrefix(model.results(enableCue)[0], "exit 0\n") {
		t.Fatal("preserved offline enable failed")
	}
	assertSwitches(moved, "BoB")
	readPeer("CLI_SKILL_SCOPE_MANUAL_PEER_READ", "CURRENT_SELF_BODY")
	const disableCue = "CLI_SKILL_SCOPE_MANUAL_DISABLE"
	model.add(disableCue, mcpRuntimeTool("sh", map[string]string{"description": "Manually disable the skill with the preserved offline CLI", "command": `"$XBOT_BIN" --root "$XBOT_ROOT" skill disable BoB ` + slug}))
	cliSkillSend(t, srv, moved, disableCue)
	if !strings.HasPrefix(model.results(disableCue)[0], "exit 0\n") {
		t.Fatal("preserved offline disable failed")
	}
	assertSwitches(moved)

	const disabledCue = "CLI_SKILL_SCOPE_DISABLED_PEER_READ"
	model.add(disabledCue, mcpRuntimeTool("sh", map[string]string{"description": "Read the manual disable from the already-running peer", "command": `"$XBOT_BIN" skill read ` + slug + ` --self --json`}))
	cliSkillSend(t, srv, "BoB", disabledCue)
	peerResult := model.results(disabledCue)[0]
	status, _, ok := strings.Cut(peerResult, "\n")
	if !ok || !exitLine.MatchString(status) || status == "exit 0" || len(commandOutput(peerResult)) > 64<<10 || !strings.Contains(strings.ToLower(commandOutput(peerResult)), "disabled") {
		t.Fatal("running peer did not clearly refuse its manually disabled skill")
	}
	assertSwitches(moved)

	// Unknown/ambiguous preflight must preserve the exact committed skill and
	// every local config, including owner fixtures and unrelated switches.
	rejectScope := func(cue, scope string) {
		t.Helper()
		paths := append(configPaths(), filepath.Join(srv.Root, "skills", slug, "SKILL.md"))
		before := make(map[string][]byte, len(paths))
		for _, path := range paths {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			before[path] = raw
		}
		model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Reject unresolved scope before committing skill or defaults", "command": `"$XBOT_BIN" skill write ` + slug + ` --description 'Must not commit' --body REJECTED_SCOPE_BODY --scope ` + shellQuote(scope) + ` --self --json`}))
		cliSkillSend(t, srv, moved, cue)
		result := model.results(cue)[0]
		status, _, ok := strings.Cut(result, "\n")
		if !ok || !exitLine.MatchString(status) || status == "exit 0" || len(commandOutput(result)) > 64<<10 || strings.TrimSpace(commandOutput(result)) == "" {
			t.Fatal("invalid scope did not return a bounded nonzero CLI refusal")
		}
		for path, want := range before {
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("invalid scope changed %s", filepath.Base(path))
			}
		}
	}
	rejectScope("CLI_SKILL_SCOPE_UNKNOWN", "NoSuchScopeBot")
	// A single real filesystem observation decides whether the existing Bot
	// creation API can represent the case collision; no synthetic Bot dirs.
	if _, err := os.Stat(filepath.Join(srv.Root, "bots", "bob", "config.json")); os.IsNotExist(err) {
		srv.CreateBot("bob", "Actual case-collision target for ambiguous scope preflight.")
		rejectScope("CLI_SKILL_SCOPE_AMBIGUOUS", "BOB")
	} else if err != nil {
		t.Fatal(err)
	}
}
