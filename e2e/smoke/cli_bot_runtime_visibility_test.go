//go:build smoke

package smoke

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// List uses current source owners; Info delegates to the dynamically attached
// Web host and its authenticated owner. Only the upstream model is scripted.
func TestCLIBotRuntimeListAndHostInfoVisibility(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const source, peer = "VisibilityWorker", "VisibilityPeer"
	const other, shared, foreign, orphan = "VisibilityOtherOwner", "VisibilityShared", "VisibilityForeign", "VisibilityOrphan"
	for _, name := range []string{source, peer, other, shared, foreign, orphan} {
		srv.CreateBot(name, "Visibility fixture duty.")
	}
	peerRaw, err := os.ReadFile(filepath.Join(srv.Root, "bots", peer, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var peerConfig struct{ Owners []string }
	if err := json.Unmarshal(peerRaw, &peerConfig); err != nil {
		t.Fatal(err)
	}
	if len(peerConfig.Owners) != 1 || peerConfig.Owners[0] == "" {
		t.Fatal("fresh authenticated API peer does not identify this run's real owner")
	}
	owner := peerConfig.Owners[0]
	const extraOwner, foreignOwner = "visibility-extra-owner", "visibility-foreign-owner"
	if owner == extraOwner || owner == foreignOwner {
		t.Fatal("controlled fixture owners collided with the actual authenticated owner")
	}
	// All fixture changes precede the source's first message/kernel. Read each
	// actual config before replacing only its owners; never print identities or
	// entire config files. Shared intersects the real owner; foreign does not
	// become visible merely because it shares an owner with Shared.
	for name, owners := range map[string][]string{
		source:  {owner, extraOwner},
		other:   {extraOwner},
		shared:  {owner, foreignOwner},
		foreign: {foreignOwner},
		orphan:  {},
	} {
		path := filepath.Join(srv.Root, "bots", name, "config.json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var cfg map[string]json.RawMessage
		if err := json.Unmarshal(raw, &cfg); err != nil {
			t.Fatal(err)
		}
		cfg["owners"], err = json.Marshal(owners)
		if err != nil {
			t.Fatal(err)
		}
		updated, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(updated, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Observe the authenticated host's actual set, including whatever Primary
	// this private root has. No seed name or fake StatusSource is substituted.
	status, hostRaw := srv.do(http.MethodGet, "/api/bots", nil)
	if status != http.StatusOK {
		t.Fatalf("read actual authenticated host Bot scope: HTTP %d", status)
	}
	var host struct{ Bots []struct{ Name string } }
	if err := json.Unmarshal(hostRaw, &host); err != nil {
		t.Fatal(err)
	}
	var hostNames []string
	for _, item := range host.Bots {
		hostNames = append(hostNames, item.Name)
	}
	slices.Sort(hostNames)
	if !slices.Contains(hostNames, source) || !slices.Contains(hostNames, peer) || !slices.Contains(hostNames, shared) || slices.Contains(hostNames, other) || slices.Contains(hostNames, foreign) || slices.Contains(hostNames, orphan) {
		t.Fatal("controlled fixtures do not express the intended authenticated host ownership scope")
	}
	wantLocal := slices.DeleteFunc(slices.Clone(hostNames), func(name string) bool { return name == source })
	wantLocal = append(wantLocal, other)
	slices.Sort(wantLocal)

	const cue = "CLI_BOT_LIST_HOST_INFO_VISIBILITY"
	model.add(cue,
		mcpRuntimeTool("sh", map[string]string{"description": "List the original source's owner-intersection peers", "command": `XBOT_BOT=` + foreign + ` "$XBOT_BIN" list --self --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Read the dynamically attached host owner status view", "command": `XBOT_BOT=` + foreign + ` "$XBOT_BIN" info --self --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Read source self status even though peer List excludes self", "command": `"$XBOT_BIN" info ` + source + ` --self --json`}),
		mcpRuntimeTool("sh", map[string]string{"description": "Refuse named Info outside the authenticated host owner scope", "command": `"$XBOT_BIN" info ` + other + ` --self --json`}),
	)
	cliSkillSend(t, srv, source, cue)
	results := model.results(cue)
	if len(results) != 4 {
		t.Fatalf("Bot visibility result count=%d", len(results))
	}
	var listed cliBotListReply
	cliBotDecode(t, results[0], &listed)
	if listed.Local == nil || listed.Remote == nil || len(*listed.Remote) != 0 || listed.Count != len(wantLocal) || listed.File != "" || listed.Error != "" {
		t.Fatal("small source List did not return complete actual local/empty remote arrays and count")
	}
	var localNames []string
	for _, item := range *listed.Local {
		localNames = append(localNames, item.Name)
		if strings.TrimSpace(item.PersonaPreview) == "" {
			t.Fatal("visible local Bot lost its duty preview")
		}
	}
	if !slices.Equal(localNames, wantLocal) {
		t.Fatal("source List lost sorted owner intersections or exposed self/foreign/orphan")
	}
	var info cliBotInfoReply
	cliBotDecode(t, results[1], &info)
	if info.Bots == nil || info.Count != len(hostNames) || info.File != "" || info.Error != "" {
		t.Fatal("Info did not return the complete dynamic authenticated host status view")
	}
	var infoNames []string
	for _, item := range *info.Bots {
		infoNames = append(infoNames, item.Bot)
		if item.State == "" {
			t.Fatal("actual host status omitted a Bot's state")
		}
	}
	slices.Sort(infoNames)
	if !slices.Equal(infoNames, hostNames) {
		t.Fatal("Info incorrectly used source List ownership instead of the dynamic Web host view")
	}
	var self cliBotInfoReply
	cliBotDecode(t, results[2], &self)
	if self.Bots == nil || len(*self.Bots) != 1 || self.Count != 1 || (*self.Bots)[0].Bot != source || (*self.Bots)[0].State == "" || self.File != "" || self.Error != "" {
		t.Fatal("host Info failed to include its owned source self, which List deliberately excludes")
	}
	refusal := results[3]
	exit, _, ok := strings.Cut(refusal, "\n")
	if !ok || !exitLine.MatchString(exit) || exit == "exit 0" || strings.TrimSpace(commandOutput(refusal)) == "" || len(commandOutput(refusal)) > 32<<10 {
		t.Fatal("named Info accepted a source-visible Bot outside the authenticated host scope")
	}
}
