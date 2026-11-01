//go:build smoke

package smoke

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Current CLI spelling is flat `list`, not a new `bot list` family. This only
// establishes the absent source-aware JSON entry on main2f; sealed baseline.
// The first candidate uses PrimaryCreateDeleteEntry below without repeating it.
func TestCLIBotRuntimeListEntry(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	const cue = "CLI_BOT_RUNTIME_LIST_ENTRY"
	model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Discover Bots through the original current source endpoint", "command": `"$XBOT_BIN" list --self --json`}))
	srv := Start(t, &Stub{srv: model.server})
	srv.CreateBot("BotListWorker", "Independent source runtime Bot list entry acceptance.")
	cliSkillSend(t, srv, "BotListWorker", cue)
	results := model.results(cue)
	if len(results) != 1 || !strings.HasPrefix(results[0], "exit 0\n") {
		t.Fatalf("actual source Bot list did not succeed: %.1024q", results)
	}
	raw := commandOutput(results[0])
	if !json.Valid([]byte(raw)) {
		t.Fatalf("actual source Bot list did not return complete runtime JSON: %.1024q", raw)
	}
}

func TestCLIBotRuntimePrimaryCreateDeleteEntry(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const peer, target, persona = "BotEntryPeer", "BotCreatedEntry", "Explicit duty from the actual Primary source."
	srv.CreateBot(peer, "Existing same-owner discovery peer.")
	// Read the real instance Primary and authenticated peer's actual owners.
	// Never send management from an ordinary worker or hard-code the seed name.
	raw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var primary struct{ Name string }
	if err := json.Unmarshal(raw, &primary); err != nil {
		t.Fatal(err)
	}
	if primary.Name == "" || primary.Name == peer {
		t.Fatal("fixture did not resolve the actual Primary")
	}
	type configView struct {
		Name, Persona, Provider, Model, Role string
		Links, Owners                        []string
		BackgroundMaxTime                    int `json:"background_max_time"`
	}
	readConfig := func(name string) configView {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(srv.Root, "bots", name, "config.json"))
		if err != nil {
			t.Fatal(err)
		}
		var cfg configView
		if err := json.Unmarshal(data, &cfg); err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	source, existing := readConfig(primary.Name), readConfig(peer)
	// Bind any missing authenticated owner before the real Primary's first
	// source wake. Empty Links are valid defaultLinks; preserve them exactly.
	if len(existing.Owners) == 0 {
		t.Fatal("actual API peer lacks source fixture bindings")
	}
	owners := slices.Clone(source.Owners)
	for _, owner := range existing.Owners {
		if owner != "" && !slices.Contains(owners, owner) {
			owners = append(owners, owner)
		}
	}
	if !slices.Equal(owners, source.Owners) {
		path := filepath.Join(srv.Root, "bots", primary.Name, "config.json")
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var cfg map[string]json.RawMessage
		if err := json.Unmarshal(before, &cfg); err != nil {
			t.Fatal(err)
		}
		cfg["owners"], _ = json.Marshal(owners)
		bound, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(bound, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
		source = readConfig(primary.Name)
	}
	if len(source.Owners) == 0 {
		t.Fatal("actual Primary lacks required owner fixture binding")
	}
	shared := false
	for _, owner := range existing.Owners {
		if owner != "" && slices.Contains(source.Owners, owner) {
			shared = true
		}
	}
	if !shared {
		t.Fatal("actual Primary is not bound to this run's authenticated source owner")
	}
	registryRaw, err := os.ReadFile(filepath.Join(srv.Root, "providers.json"))
	if err != nil {
		t.Fatal(err)
	}
	var registry struct {
		Providers []struct {
			Name   string
			Models []struct{ Name string }
		}
	}
	if err := json.Unmarshal(registryRaw, &registry); err != nil {
		t.Fatal(err)
	}
	defaultModel := ""
	for _, provider := range registry.Providers {
		if provider.Name == "deepseek" && len(provider.Models) > 0 {
			defaultModel = provider.Models[0].Name
		}
	}
	if defaultModel == "" {
		t.Fatal("private instance has no actual global default binding")
	}
	const cue = "CLI_BOT_PRIMARY_CREATE_ENTRY"
	model.add(cue,
		mcpRuntimeTool("sh", map[string]string{"description": "List visible peers from the actual Primary source", "command": `"$XBOT_BIN" list --self --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Read actual source host status and bounded steps", "command": `"$XBOT_BIN" info ` + shellQuote(primary.Name) + ` --self --steps 2 --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Create a Bot with explicit duty from the actual Primary", "command": `"$XBOT_BIN" new ` + target + ` --self --persona ` + shellQuote("  "+persona+"\n") + ` --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Discover the actual newly created Bot through the same source", "command": `"$XBOT_BIN" list --self --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Read the new cold Bot through the original dynamic host status source", "command": `"$XBOT_BIN" info ` + target + ` --self --json`}),
	)
	cliSkillSend(t, srv, primary.Name, cue)
	results := model.results(cue)
	checkList := func(result string, wantTarget bool) {
		t.Helper()
		var reply cliBotListReply
		cliBotDecode(t, result, &reply)
		if reply.Local == nil || reply.Remote == nil || reply.File != "" || reply.Error != "" || reply.Count != len(*reply.Local)+len(*reply.Remote) {
			t.Fatal("small Bot discovery did not return complete actual arrays/count")
		}
		peerSeen, targetSeen := false, false
		for _, local := range *reply.Local {
			if local.Name == primary.Name {
				t.Fatal("source list included itself")
			}
			if local.Name == peer {
				peerSeen = true
			}
			if local.Name == target {
				targetSeen = true
				if local.PersonaPreview != persona {
					t.Fatal("created Bot duty preview lost its exact short text")
				}
			}
		}
		if !peerSeen || targetSeen != wantTarget {
			t.Fatal("actual Bot discovery lost the peer or target lifecycle effect")
		}
	}
	checkInfo := func(result, name string) {
		t.Helper()
		var info cliBotInfoReply
		cliBotDecode(t, result, &info)
		if info.Bots == nil || len(*info.Bots) != 1 || info.Count != 1 || (*info.Bots)[0].Bot != name || (*info.Bots)[0].State == "" || info.File != "" || info.Error != "" {
			t.Fatal("named actual host status did not preserve the requested Bot")
		}
		if info.Steps != nil && len(*info.Steps) > 2 {
			t.Fatal("named info ignored the bounded steps request")
		}
	}
	checkList(results[0], false)
	checkInfo(results[1], primary.Name)
	var created cliBotCreateReply
	cliBotDecode(t, results[2], &created)
	if created.Name != target || !created.Applied || created.Stage != "complete" || created.Error != "" {
		t.Fatal("source creation did not report the actual complete mutation")
	}
	checkList(results[3], true)
	checkInfo(results[4], target)
	cfg := readConfig(target)
	if cfg.Name != target || cfg.Persona != persona || !slices.Equal(cfg.Owners, source.Owners) || !slices.Equal(cfg.Links, source.Links) || cfg.Provider != "deepseek" || cfg.Model != defaultModel || cfg.Role != "" || cfg.BackgroundMaxTime != 0 {
		t.Fatal("new Bot lost actual full source owner/link inheritance or global default binding")
	}
	duty, err := os.ReadFile(filepath.Join(srv.Root, "bots", target, "persona.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(duty)) != persona {
		t.Fatal("actual created persona file lost the explicit normalized duty")
	}
	state, err := os.ReadFile(filepath.Join(srv.Root, "bots", target, "state.json"))
	if err != nil || !json.Valid(state) {
		t.Fatal("actual created Bot state is missing or invalid")
	}
	const removeCue = "CLI_BOT_PRIMARY_DELETE_ENTRY"
	model.add(removeCue,
		mcpRuntimeTool("sh", map[string]string{"description": "Delete the actual created Bot through its host lifecycle", "command": `"$XBOT_BIN" delete ` + target + ` --self --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Discover peers after actual host deletion", "command": `"$XBOT_BIN" list --self --json`}),
	)
	cliSkillSend(t, srv, primary.Name, removeCue)
	removed := model.results(removeCue)
	var receipt cliBotDeleteReply
	cliBotDecode(t, removed[0], &receipt)
	if receipt.Name != target || !receipt.Removed || receipt.Stage != "complete" || receipt.Error != "" {
		t.Fatal("source deletion did not report the actual removed fact")
	}
	checkList(removed[1], false)
	if _, err := os.Stat(filepath.Join(srv.Root, "bots", target)); !os.IsNotExist(err) {
		t.Fatal("deleted target still has an actual Bot directory")
	}
	afterPrimary, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var unchanged struct{ Name string }
	if err := json.Unmarshal(afterPrimary, &unchanged); err != nil || unchanged.Name != primary.Name {
		t.Fatal("new/delete changed the actual source Primary")
	}
}
