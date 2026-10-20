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

// With the table's cards off (the default for serve and chat, ADR 0019),
// calls the table would confirm run without a card; the links card, which
// collects a connector's keys, and an explicit machine "ask" still ask.
func TestPermissiveAsksOnlyWhereACardIsNeeded(t *testing.T) {
	k := &Kernel{Bot: testBot(t, link.NameTools), Permissive: true}
	k.ensureTurn().turnTrusted = true
	checked := 0
	for _, tool := range []string{"mcp_x_send", "http_request", "delete_file", "propose_persona"} {
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
	// The machine switch and the guard on outside turns are off too.
	k.Machine = func() (string, error) { return "ask", nil }
	if needsCard(t, k, "shell", `{"command":"ls"}`) {
		t.Error("machine ask still asked with permissions off")
	}
	k.Permissive = false
	if !needsCard(t, k, "shell", `{"command":"ls"}`) {
		t.Error("machine ask was ignored with permissions on")
	}
	k.Permissive = true
	k.Machine = nil
	k.ensureTurn().turnTrusted = false
	if needsCard(t, k, "shell", `{"command":"echo x >> memory/memory.md"}`) {
		t.Error("a turn from outside still asks to touch memory")
	}
	k.Permissive = false
	if !needsCard(t, k, "shell", `{"command":"echo x >> memory/memory.md"}`) {
		t.Error("with permissions on, a turn from outside touching memory must ask")
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
