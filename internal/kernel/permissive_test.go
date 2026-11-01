package kernel

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"xbot/internal/link"
	"xbot/internal/machine"
	"xbot/internal/permissions"
	"xbot/internal/review"
	"xbot/internal/tools"
)

type namedTool struct{ name string }

func (n namedTool) Name() string        { return n.name }
func (n namedTool) Description() string { return n.name }
func (n namedTool) Parameters() any     { return map[string]any{"type": "object"} }
func (n namedTool) Confirm(json.RawMessage) (bool, string, error) {
	return false, n.name, nil
}
func (n namedTool) Run(context.Context, json.RawMessage) (string, error) { return "ok", nil }

func needsCard(t *testing.T, k *Kernel, tool, args string) bool {
	t.Helper()
	need, _, err := reviewedTool{Tool: namedTool{tool}, k: k}.ConfirmContext(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	return need
}

// With the table's cards off (permanent for serve/chat, ADR 0019),
// calls run without permission cards; links still collects connector keys.
func TestPermissiveAsksOnlyWhereACardIsNeeded(t *testing.T) {
	k := &Kernel{Bot: testBot(t, link.NameTools), Permissive: true}
	k.ensureTurn().turnTrusted = true
	checked := 0
	for _, tool := range []string{"mcp_x_send", "http_request", "delete_file", "bot_propose_persona"} {
		if !review.NeedsOwner(tool, json.RawMessage(`{"method":"POST","url":"https://x"}`)) {
			continue
		}
		checked++
		if needsCard(t, k, tool, `{"method":"POST","url":"https://x","path":"a.txt"}`) {
			t.Errorf("%s asked with the cards off", tool)
		}
		k.Permissive = false
		if !needsCard(t, k, tool, `{"method":"POST","url":"https://x","path":"a.txt"}`) {
			t.Errorf("%s did not ask with the table on", tool)
		}
		k.Permissive = true
	}
	if checked < 3 {
		t.Fatalf("only %d tools the table confirms", checked)
	}
	if !needsCard(t, k, "links", `{"action":"install","id":"telegram"}`) {
		t.Error("the links card was dropped")
	}
	if needsCard(t, k, "restart_xbot", `{}`) {
		t.Error("restart asked with the cards off")
	}
	// The machine switch and the guard on outside turns are off too.
	k.Machine = func() (string, error) { return "ask", nil }
	if needsCard(t, k, "sh", `{"description":"Verify the scenario behavior","command":"ls"}`) {
		t.Error("machine ask still asked with permissions off")
	}
	k.Permissive = false
	if !needsCard(t, k, "sh", `{"description":"Verify the scenario behavior","command":"ls"}`) {
		t.Error("machine ask was ignored with permissions on")
	}
	k.Permissive = true
	k.Machine = nil
	k.ensureTurn().turnTrusted = false
	if needsCard(t, k, "sh", `{"description":"Verify the scenario behavior","command":"echo x >> memory/memory.md"}`) {
		t.Error("a turn from outside still asks to touch memory")
	}
	k.Permissive = false
	if !needsCard(t, k, "sh", `{"description":"Verify the scenario behavior","command":"echo x >> memory/memory.md"}`) {
		t.Error("with permissions on, a turn from outside touching memory must ask")
	}
}

func TestPermissiveProtectedCallsNeverAsk(t *testing.T) {
	k, work := protectedKernel(t)
	k.Permissive = true
	k.PermissionsOn = func() bool { return true }
	k.Machine = func() (string, error) { return machine.Ask, nil }
	for _, tool := range []tools.Tool{&tools.ReadFile{Root: work}, &tools.WriteFile{Root: work}, &tools.EditFile{Root: work}, &tools.Shell{}} {
		args, err := json.Marshal(map[string]string{"path": k.Bot.VaultKeyPath(), "command": "cat " + k.Bot.VaultKeyPath(), "old_string": "fake", "new_string": "replacement"})
		if err != nil {
			t.Fatal(err)
		}
		if !k.protectedCall(context.Background(), tool, args) {
			t.Fatalf("%s did not exercise a protected path", tool.Name())
		}
		need, _, err := k.reviewCall(context.Background(), tool, args, "protected operation")
		if err != nil || need {
			t.Fatalf("%s: need=%v err=%v", tool.Name(), need, err)
		}
	}
}

func TestRestartDoesNotRequireTrustedUserTurnOrConfirmation(t *testing.T) {
	k := &Kernel{Bot: testBot(t, link.NameTools), Permissive: true}
	for _, trusted := range []bool{false, true} {
		for _, permissive := range []bool{false, true} {
			k.ensureTurn().turnTrusted = trusted
			k.Permissive = permissive
			if needsCard(t, k, "restart_xbot", `{}`) {
				t.Fatalf("restart asked: trusted=%v permissive=%v", trusted, permissive)
			}
		}
	}
}

// With permission checks off, a stored machine mode reads as allow; turning
// them on brings it back.
func TestMachineSwitchFollowsPermissions(t *testing.T) {
	root := t.TempDir()
	if err := machine.Set(root, machine.Local, machine.Off); err != nil {
		t.Fatal(err)
	}
	if mode, _ := machine.Get(root, machine.Local); mode != machine.Allow {
		t.Fatalf("mode %q with permissions off", mode)
	}
	if err := permissions.Set(root, true); err != nil {
		t.Fatal(err)
	}
	if mode, _ := machine.Get(root, machine.Local); mode != machine.Off {
		t.Fatalf("mode %q with permissions on", mode)
	}
}

func TestConfirmTableSwitch(t *testing.T) {
	root := t.TempDir()
	if permissions.On(root) {
		t.Fatal("cards on without a setting")
	}
	if err := permissions.Set(root, true); err != nil {
		t.Fatal(err)
	}
	if !permissions.On(root) {
		t.Fatal("on did not stick")
	}
	if err := permissions.Set(root, false); err != nil {
		t.Fatal(err)
	}
	if permissions.On(root) {
		t.Fatal("off did not stick")
	}
}

// With the cards off the model is not told to ask before a step the owner
// named; with them on it still is.
func TestPermissiveRuleDropsTheSecondQuestion(t *testing.T) {
	k := &Kernel{Bot: testBot(t, link.NameTools), Permissive: true}
	rules, err := k.rulesText()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rules, OwnAskActionRule) || strings.Contains(rules, ActionRule) {
		t.Fatal("cards off: the rule still asks before named steps")
	}
	if !strings.Contains(rules, EventMemoryRule) {
		t.Fatal("cards off: events are not told they may change memory")
	}
	k.Permissive = false
	if rules, _ = k.rulesText(); !strings.Contains(rules, ActionRule) || strings.Contains(rules, EventMemoryRule) {
		t.Fatal("cards on: the rules changed")
	}
}

// Production always-allow kernels, including existing subagent approvers,
// cannot be switched back to permission cards by permissions.json.
func TestLivePermissionSwitch(t *testing.T) {
	b := testBot(t, link.NameTools)
	k := &Kernel{Bot: b, Permissive: true, PermissionsOn: func() bool { return permissions.On(b.Root) }}
	k.ensureTurn().turnTrusted = true
	asked := 0
	k.BackgroundApprove = func(context.Context, string, string) (bool, error) { asked++; return true, nil }
	approve := k.subApprover()
	check := func(on bool) {
		t.Helper()
		if got := needsCard(t, k, "delete_file", `{"path":"a.txt"}`); got {
			t.Fatalf("delete needsCard=%v, on=%v", got, on)
		}
		for _, tool := range []string{"http_request", "mcp_x_send", "bot_propose_persona", "sh"} {
			if needsCard(t, k, tool, `{"method":"POST","command":"rm a.txt","text":"new persona"}`) {
				t.Fatalf("%s asked with permissions on=%v", tool, on)
			}
		}
		if !needsCard(t, k, "links", `{"action":"install","id":"telegram"}`) {
			t.Fatal("links key input card was dropped")
		}
		before := asked
		if _, err := approve(context.Background(), "delete_file", "delete a.txt"); err != nil {
			t.Fatal(err)
		}
		if asked > before {
			t.Fatalf("subagent asked=%d, on=%v", asked-before, on)
		}
	}
	check(false)
	if err := permissions.Set(b.Root, true); err != nil {
		t.Fatal(err)
	}
	check(true)
	if err := permissions.Set(b.Root, false); err != nil {
		t.Fatal(err)
	}
	check(false)
}
