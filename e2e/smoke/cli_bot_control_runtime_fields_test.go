//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Only the upstream model is scripted. Persisted fixture changes precede the
// restart that captures the actual Primary resources; no disk edit simulates a
// live update to a handle already held by the server.
func TestCLIBotControlRuntimeSelfFieldsAndLocalVisibility(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const peer, shared, foreign, orphan = "ControlFieldsPeer", "ControlFieldsShared", "ControlFieldsForeign", "ControlFieldsOrphan"
	for _, name := range []string{peer, shared, foreign, orphan} {
		srv.CreateBot(name, "Preserve the actual persisted duty and binding during Bot control.")
	}
	primaryPath := filepath.Join(srv.Root, "primary.json")
	primaryRaw, err := os.ReadFile(primaryPath)
	if err != nil {
		t.Fatal(err)
	}
	var primary struct{ Name string }
	if err := json.Unmarshal(primaryRaw, &primary); err != nil || primary.Name == "" {
		t.Fatal("fixture did not resolve the real root Primary A")
	}
	a := primary.Name
	type configView struct {
		Name, Persona, Provider, Model, Description, Timezone string
		Role, Avatar                                          string
		AvatarSeed                                            string `json:"avatar_seed"`
		PersonaCard                                           string `json:"persona_card"`
		Owners, Links                                         []string
		DisabledSkills                                        []string `json:"disabled_skills"`
		BackgroundMaxTime                                     int      `json:"background_max_time"`
		Pinned                                                bool
	}
	readConfig := func(name string) (configView, []byte) {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(srv.Root, "bots", name, "config.json"))
		if err != nil {
			t.Fatal(err)
		}
		var cfg configView
		if err := json.Unmarshal(raw, &cfg); err != nil {
			t.Fatal(err)
		}
		return cfg, raw
	}
	actualPeer, _ := readConfig(peer)
	source, _ := readConfig(a)
	if len(actualPeer.Owners) != 1 || actualPeer.Owners[0] == "" || !slices.Contains(source.Owners, actualPeer.Owners[0]) {
		t.Fatal("actual Primary and API peer lack the authenticated owner's intersection")
	}
	owner := actualPeer.Owners[0]
	const extraOwner, foreignOwner = "control-fields-extra-owner", "control-fields-foreign-owner"
	if owner == extraOwner || owner == foreignOwner || slices.Contains(source.Owners, extraOwner) || slices.Contains(source.Owners, foreignOwner) {
		t.Fatal("controlled owner fixture collides with actual source ownership")
	}
	// These are ownership data fixtures, never sender identities. Shared has
	// only the source's extra owner; visibility cannot rely on the login owner.
	for name, owners := range map[string][]string{
		a:       append(slices.Clone(source.Owners), extraOwner),
		shared:  {extraOwner},
		foreign: {foreignOwner},
		orphan:  {},
	} {
		_, raw := readConfig(name)
		var cfg map[string]json.RawMessage
		if err := json.Unmarshal(raw, &cfg); err != nil {
			t.Fatal(err)
		}
		fields := map[string]any{
			"owners": owners, "links": []string{}, "description": "Supported profile fields survive pin and same-primary writes.",
			"timezone": "UTC", "role": "dev", "background_max_time": 5400,
			"avatar_seed": "control-fields-preserved", "disabled_skills": []string{"control-preserved-skill"},
			"pinned": name == a || name == orphan,
		}
		for field, value := range fields {
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			cfg[field] = encoded
		}
		updated, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(srv.Root, "bots", name, "config.json"), append(updated, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// serve captures RootPrimary during Start, so all fixture writes must be
	// followed by this private restart before the first source runtime call.
	srv.Restart()
	baseline := make(map[string]configView)
	personas := make(map[string][]byte)
	names := []string{a, peer, shared, foreign, orphan}
	for _, name := range names {
		baseline[name], _ = readConfig(name)
		data, err := os.ReadFile(filepath.Join(srv.Root, "bots", name, "persona.md"))
		if err != nil {
			t.Fatal(err)
		}
		personas[name] = data
	}
	if !slices.Equal(baseline[a].Owners, append(slices.Clone(source.Owners), extraOwner)) || !slices.Equal(baseline[shared].Owners, []string{extraOwner}) {
		t.Fatal("restarted persisted fixture lost the complete source owners or extra-owner-only target")
	}
	for _, name := range []string{a, shared, foreign, orphan} {
		cfg := baseline[name]
		if len(cfg.Links) != 0 || cfg.Description != "Supported profile fields survive pin and same-primary writes." || cfg.Timezone != "UTC" || cfg.Role != "dev" || cfg.BackgroundMaxTime != 5400 || cfg.AvatarSeed != "control-fields-preserved" || !slices.Equal(cfg.DisabledSkills, []string{"control-preserved-skill"}) || cfg.Pinned != (name == a || name == orphan) {
			t.Fatal("restart did not capture the configured supported fields and legal empty Links")
		}
	}
	assertConfig := func(name string, pinned bool) {
		t.Helper()
		got, _ := readConfig(name)
		want := baseline[name]
		want.Pinned = pinned
		if !reflect.DeepEqual(got, want) {
			t.Fatal("control changed supported profile/owners/links/persona/binding/role/skills/card fields beyond Pinned")
		}
		data, err := os.ReadFile(filepath.Join(srv.Root, "bots", name, "persona.md"))
		if err != nil || !bytes.Equal(data, personas[name]) {
			t.Fatal("control changed an actual persisted persona file")
		}
	}
	assertPrimary := func() {
		t.Helper()
		raw, err := os.ReadFile(primaryPath)
		if err != nil {
			t.Fatal(err)
		}
		var actual struct{ Name string }
		if err := json.Unmarshal(raw, &actual); err != nil || actual.Name != a {
			t.Fatal("self/same-primary control changed the actual Primary pointer away from A")
		}
	}
	run := func(cue, command string) string {
		t.Helper()
		model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Exercise actual Primary self and visible-local Bot control", "command": command}))
		cliSkillSend(t, srv, a, cue)
		results := model.results(cue)
		if len(results) != 1 || results[0] == "" {
			t.Fatal("actual Primary source did not execute its control operation")
		}
		return results[0]
	}
	var unpinned cliBotControlPinReply
	cliBotDecode(t, run("CLI_CONTROL_FIELDS_SELF_UNPIN", `"$XBOT_BIN" unpin `+shellQuote(a)+` --self --json`), &unpinned)
	if unpinned.Name != a || unpinned.Pinned || !unpinned.Applied || unpinned.Stage != "complete" || unpinned.Error != "" {
		t.Fatal("actual Primary self-unpin lost its saved false Pinned ACK")
	}
	assertPrimary()
	assertConfig(a, false)
	beforeSame, err := os.Stat(primaryPath)
	if err != nil {
		t.Fatal(err)
	}
	var same cliBotControlPrimaryReply
	cliBotDecode(t, run("CLI_CONTROL_FIELDS_SAME_PRIMARY", `"$XBOT_BIN" primary set `+shellQuote(a)+` --self --json`), &same)
	if same.Name != a || same.Primary != a || !same.Applied || same.Stage != "complete" || same.Error != "" {
		t.Fatal("same-primary must ACK its actual pointer write as applied, without requiring an identity change")
	}
	afterSame, err := os.Stat(primaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(beforeSame, afterSame) {
		t.Fatal("same-primary skipped the original atomic pointer write")
	}
	assertPrimary()
	assertConfig(a, true)
	var pinned cliBotControlPinReply
	cliBotDecode(t, run("CLI_CONTROL_FIELDS_EXTRA_OWNER_LOCAL", `"$XBOT_BIN" pin `+shared+` --self --json`), &pinned)
	if pinned.Name != shared || !pinned.Pinned || !pinned.Applied || pinned.Stage != "complete" || pinned.Error != "" {
		t.Fatal("actual source failed to pin a local target visible only through its extra owner")
	}
	assertPrimary()
	assertConfig(shared, true)
	// One finite refusal per excluded target class. No device/cache fixture or
	// remote transport is needed to reject a remote-shaped management target.
	for _, refusal := range []struct {
		cue, target, action string
		pinned              bool
	}{
		{"CLI_CONTROL_FIELDS_FOREIGN", foreign, "pin", true},
		{"CLI_CONTROL_FIELDS_ORPHAN", orphan, "unpin", false},
		{"CLI_CONTROL_FIELDS_REMOTE", peer + "@control-device", "primary set", false},
	} {
		before := make(map[string][]byte)
		for _, name := range names {
			_, before[name] = readConfig(name)
		}
		pointer, err := os.ReadFile(primaryPath)
		if err != nil {
			t.Fatal(err)
		}
		result := run(refusal.cue, `"$XBOT_BIN" `+refusal.action+` `+shellQuote(refusal.target)+` --self --json`)
		exit, _, ok := strings.Cut(result, "\n")
		if !ok || !exitLine.MatchString(exit) || exit == "exit 0" {
			t.Fatal("excluded local/remote-shaped target did not produce a real nonzero refusal")
		}
		if refusal.action == "primary set" {
			var reply cliBotControlPrimaryReply
			if err := json.NewDecoder(strings.NewReader(commandOutput(result))).Decode(&reply); err != nil {
				t.Fatal("refused primary control lost its complete typed ACK")
			}
			if reply.Name != refusal.target || reply.Primary != "" || reply.Applied || reply.Stage != "validation" || reply.Error == "" {
				t.Fatal("refused remote-shaped primary target claimed a pointer write")
			}
		} else {
			var reply cliBotControlPinReply
			if err := json.NewDecoder(strings.NewReader(commandOutput(result))).Decode(&reply); err != nil {
				t.Fatal("refused pin control lost its complete typed ACK")
			}
			if reply.Name != refusal.target || reply.Pinned != refusal.pinned || reply.Applied || reply.Stage != "validation" || reply.Error == "" {
				t.Fatal("refused local target lost requested Pinned or claimed a mutation")
			}
		}
		for _, name := range names {
			_, after := readConfig(name)
			if !bytes.Equal(before[name], after) {
				t.Fatal("refused control changed an actual Bot configuration")
			}
		}
		afterPointer, err := os.ReadFile(primaryPath)
		if err != nil || !bytes.Equal(pointer, afterPointer) {
			t.Fatal("refused control changed the actual Primary pointer")
		}
	}
	for _, name := range names {
		wantPinned := baseline[name].Pinned
		if name == a || name == shared {
			wantPinned = true
		}
		assertConfig(name, wantPinned)
	}
}
