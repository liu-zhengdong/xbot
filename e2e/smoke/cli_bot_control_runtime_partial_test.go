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

// Force the original post-pointer SaveConfig seam with a real filesystem error,
// never a mocked handler, synthetic receipt or rollback of the actual pointer.
func TestCLIBotControlRuntimePrimaryPinPartial(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const b = "ControlPartialPeer"
	srv.CreateBot(b, "Explicit real target for post-pointer pin failure acceptance.")
	var primary struct{ Name string }
	raw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &primary); err != nil || primary.Name == "" || primary.Name == b {
		t.Fatal("could not resolve actual Primary A")
	}
	a := primary.Name
	type configView struct {
		Owners []string
		Pinned bool
	}
	readConfig := func(name string) configView {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(srv.Root, "bots", name, "config.json"))
		if err != nil {
			t.Fatal(err)
		}
		var cfg configView
		if err := json.Unmarshal(raw, &cfg); err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	beforeA, beforeB := readConfig(a), readConfig(b)
	shared := false
	for _, owner := range beforeA.Owners {
		shared = shared || owner != "" && slices.Contains(beforeB.Owners, owner)
	}
	if !shared || beforeB.Pinned {
		t.Fatal("partial fixture requires actual visible and initially unpinned B")
	}
	run := func(source, cue, command string) string {
		t.Helper()
		model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Observe actual post-pointer partial and fresh source qualification", "command": command}))
		cliSkillSend(t, srv, source, cue)
		results := model.results(cue)
		if len(results) != 1 || len(commandOutput(results[0])) > 32<<10 {
			t.Fatal("partial call did not return one bounded actual source result")
		}
		return results[0]
	}
	warm := run(b, "CLI_CONTROL_PARTIAL_B_ORIGINAL", `printf '%s\n' "$XBOT_ENDPOINT"`)
	if !strings.HasPrefix(warm, "exit 0\n") || strings.TrimSpace(commandOutput(warm)) == "" {
		t.Fatal("B did not publish its original nonprimary source endpoint")
	}
	endpointB := strings.TrimSpace(commandOutput(warm))
	// Create/SetOwners already left this real lock file. Observe the intended
	// absolute regular file, then replace only it with a directory: Load still
	// succeeds, primary.json still writes, original SaveConfig cannot open it.
	lockPath := filepath.Join(srv.Root, "bots", b, "config.json.lock")
	info, err := os.Lstat(lockPath)
	if err != nil || !filepath.IsAbs(lockPath) || !info.Mode().IsRegular() {
		t.Fatal("expected the actual target config lock file before the scoped FS fault")
	}
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(lockPath, 0700); err != nil {
		t.Fatal(err)
	}
	partial := run(a, "CLI_CONTROL_PARTIAL_TRANSFER", `"$XBOT_BIN" primary set `+b+` --self --json`)
	line, _, ok := strings.Cut(partial, "\n")
	if !ok || !exitLine.MatchString(line) || line == "exit 0" {
		t.Fatal("actual pin write error did not produce nonzero exit")
	}
	var receipt cliBotControlPrimaryReply
	if err := json.NewDecoder(strings.NewReader(commandOutput(partial))).Decode(&receipt); err != nil {
		t.Fatal("post-pointer failure lost its actual typed partial receipt")
	}
	if receipt.Name != b || receipt.Primary != b || !receipt.Applied || receipt.Stage != "pin" || receipt.Error == "" {
		t.Fatal("post-pointer pin failure falsely reported uncommitted Primary")
	}
	pointer := func() string {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
		if err != nil {
			t.Fatal(err)
		}
		var actual struct{ Name string }
		if err := json.Unmarshal(raw, &actual); err != nil {
			t.Fatal(err)
		}
		return actual.Name
	}
	if pointer() != b || readConfig(b).Pinned || readConfig(a).Pinned != beforeA.Pinned {
		t.Fatal("actual pointer/pin state did not preserve true two-stage partial")
	}
	beforeDenied, err := os.ReadFile(filepath.Join(srv.Root, "bots", a, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	denied := run(a, "CLI_CONTROL_PARTIAL_A_LOST", `"$XBOT_BIN" unpin `+shellQuote(a)+` --self --json`)
	line, _, ok = strings.Cut(denied, "\n")
	if !ok || !exitLine.MatchString(line) || line == "exit 0" {
		t.Fatal("old A retained Primary after error despite the actual committed pointer")
	}
	var refusal cliBotControlPinReply
	if err := json.NewDecoder(strings.NewReader(commandOutput(denied))).Decode(&refusal); err != nil {
		t.Fatal(err)
	}
	if refusal.Applied || refusal.Stage != "validation" || refusal.Error == "" {
		t.Fatal("old A denied call claimed a write")
	}
	afterDenied, err := os.ReadFile(filepath.Join(srv.Root, "bots", a, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(afterDenied) != string(beforeDenied) {
		t.Fatal("denied old source changed actual A configuration")
	}
	afterEndpoint := run(b, "CLI_CONTROL_PARTIAL_B_SAME_ENDPOINT", `printf '%s\n' "$XBOT_ENDPOINT"`)
	if !strings.HasPrefix(afterEndpoint, "exit 0\n") || strings.TrimSpace(commandOutput(afterEndpoint)) != endpointB {
		t.Fatal("B required a replacement endpoint after actual partial transfer")
	}
	var managed cliBotControlPinReply
	cliBotDecode(t, run(b, "CLI_CONTROL_PARTIAL_B_NOW_MANAGES", `"$XBOT_BIN" unpin `+shellQuote(a)+` --self --json`), &managed)
	if managed.Name != a || managed.Pinned || !managed.Applied || managed.Stage != "complete" || managed.Error != "" || readConfig(a).Pinned {
		t.Fatal("actual B did not gain management while its own pin stage remained failed")
	}
	if pointer() != b || readConfig(b).Pinned {
		t.Fatal("fixture rolled back pointer or silently repaired failed target pin")
	}
}
