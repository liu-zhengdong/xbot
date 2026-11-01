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

// Existing CLI spelling is flat pin NAME, not a new control command family.
// Only establish the missing --self entry; ADR0053 will fix DTOs and A->B
// primary transfer receipts/qualification separately after its shape review.
func TestCLIBotControlRuntimePinEntry(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const target = "PinEntryPeer"
	srv.CreateBot(target, "Actual authenticated peer for the narrow pin runtime entry.")
	raw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var primary struct{ Name string }
	if err := json.Unmarshal(raw, &primary); err != nil || primary.Name == "" {
		t.Fatal("could not resolve the actual Primary source")
	}
	readOwners := func(name string) []string {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(srv.Root, "bots", name, "config.json"))
		if err != nil {
			t.Fatal(err)
		}
		var config struct{ Owners []string }
		if err := json.Unmarshal(raw, &config); err != nil {
			t.Fatal(err)
		}
		return config.Owners
	}
	shared := false
	targetOwners := readOwners(target)
	for _, owner := range readOwners(primary.Name) {
		shared = shared || owner != "" && slices.Contains(targetOwners, owner)
	}
	if !shared {
		t.Fatal("actual Primary and peer lack this run's authenticated owner intersection")
	}
	const cue = "CLI_BOT_CONTROL_PIN_ENTRY"
	model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Pin a real visible peer from the actual current Primary source", "command": `"$XBOT_BIN" pin ` + target + ` --self`}))
	cliSkillSend(t, srv, primary.Name, cue)
	results := model.results(cue)
	if len(results) != 1 || !strings.HasPrefix(results[0], "exit 0\n") {
		t.Fatalf("actual source pin runtime entry did not succeed: %.1024q", results)
	}
}

func TestCLIBotControlRuntimePrimaryTransferEntry(t *testing.T) {
	cliSkillIsolation(t)
	model := newMCPRuntimeModel(t)
	srv := Start(t, &Stub{srv: model.server})
	const b = "ControlTransferPeer"
	srv.CreateBot(b, "Actual authenticated future Primary for source capability transfer.")
	var primary struct{ Name string }
	primaryRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(primaryRaw, &primary); err != nil || primary.Name == "" || primary.Name == b {
		t.Fatal("fixture did not resolve actual Primary A")
	}
	a := primary.Name
	type configView struct {
		Name   string
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
	if !shared {
		t.Fatal("actual Primary A and future Primary B lack the real authenticated owner intersection")
	}
	run := func(source, cue, command string) string {
		t.Helper()
		model.add(cue, mcpRuntimeTool("sh", map[string]string{"description": "Execute fixed source pin/primary entry acceptance", "command": command}))
		cliSkillSend(t, srv, source, cue)
		results := model.results(cue)
		if len(results) != 1 || results[0] == "" {
			t.Fatal("actual source did not execute its fixed operation")
		}
		if len(commandOutput(results[0])) > 32<<10 {
			t.Fatal("short control ACK exceeded actual native Shell32KiB")
		}
		return results[0]
	}
	endpoint := func(source, cue string) string {
		t.Helper()
		result := run(source, cue, `printf '%s\n' "$XBOT_ENDPOINT"`)
		if !strings.HasPrefix(result, "exit 0\n") {
			t.Fatal("actual source endpoint observation failed")
		}
		value := strings.TrimSpace(commandOutput(result))
		if value == "" {
			t.Fatal("actual source has no original endpoint")
		}
		return value
	}
	// Warm B while it is not Primary, retaining that actual listener throughout.
	// This must not rely on creating its handlers only after gaining Primary.
	endpointB := endpoint(b, "CLI_CONTROL_B_ORIGINAL_ENDPOINT")
	endpointA := endpoint(a, "CLI_CONTROL_A_ORIGINAL_ENDPOINT")
	pin := func(result, name string, want bool) {
		t.Helper()
		var reply cliBotControlPinReply
		cliBotDecode(t, result, &reply)
		if reply.Name != name || reply.Pinned != want || !reply.Applied || reply.Stage != "complete" || reply.Error != "" {
			t.Fatal("source pin/unpin lost its exact typed saved result")
		}
		if got := readConfig(name).Pinned; got != want {
			t.Fatal("actual target config did not persist requested Pinned")
		}
	}
	pin(run(a, "CLI_CONTROL_A_PIN_B", `"$XBOT_BIN" pin `+b+` --self --json`), b, true)
	pin(run(a, "CLI_CONTROL_A_UNPIN_B", `"$XBOT_BIN" unpin `+b+` --self --json`), b, false)
	var transfer cliBotControlPrimaryReply
	cliBotDecode(t, run(a, "CLI_CONTROL_A_TRANSFER_B", `"$XBOT_BIN" primary set `+b+` --self --json`), &transfer)
	if transfer.Name != b || transfer.Primary != b || !transfer.Applied || transfer.Stage != "complete" || transfer.Error != "" {
		t.Fatal("source transfer lost its actual pointer-write receipt")
	}
	actualRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var actual struct{ Name string }
	if err := json.Unmarshal(actualRaw, &actual); err != nil || actual.Name != b {
		t.Fatal("actual primary pointer did not transfer to B")
	}
	if !readConfig(b).Pinned || readConfig(a).Pinned != beforeA.Pinned {
		t.Fatal("transfer failed to pin B or changed old A Pinned")
	}
	if endpoint(a, "CLI_CONTROL_A_ENDPOINT_AFTER_TRANSFER") != endpointA {
		t.Fatal("transfer replaced A original source endpoint")
	}
	beforeDenied, err := os.ReadFile(filepath.Join(srv.Root, "bots", b, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	denied := run(a, "CLI_CONTROL_A_MUST_LOSE", `XBOT_BOT=`+b+` "$XBOT_BIN" unpin `+b+` --self --json`)
	line, _, ok := strings.Cut(denied, "\n")
	if !ok || !exitLine.MatchString(line) || line == "exit 0" {
		t.Fatal("old A retained management or diagnostic B became its identity")
	}
	var refusal cliBotControlPinReply
	if err := json.NewDecoder(strings.NewReader(commandOutput(denied))).Decode(&refusal); err != nil {
		t.Fatal("old A refusal lost its typed commit status")
	}
	if refusal.Name != b || refusal.Pinned || refusal.Applied || refusal.Stage != "validation" || refusal.Error == "" {
		t.Fatal("old A refusal claimed a mutation or wrong requested false value")
	}
	afterDenied, err := os.ReadFile(filepath.Join(srv.Root, "bots", b, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(afterDenied) != string(beforeDenied) {
		t.Fatal("old A denied call changed B definition")
	}
	if endpoint(b, "CLI_CONTROL_B_ENDPOINT_AFTER_TRANSFER") != endpointB {
		t.Fatal("B needed a replacement endpoint to gain Primary")
	}
	pin(run(b, "CLI_CONTROL_B_NOW_MANAGES", `XBOT_BOT=`+a+` "$XBOT_BIN" unpin `+shellQuote(a)+` --self --json`), a, false)
	finalRaw, err := os.ReadFile(filepath.Join(srv.Root, "primary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var final struct{ Name string }
	if err := json.Unmarshal(finalRaw, &final); err != nil || final.Name != b {
		t.Fatal("fresh B management changed its Primary pointer")
	}
}
