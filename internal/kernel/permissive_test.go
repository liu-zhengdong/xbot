package kernel

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"xbot/internal/cliops"
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

// linksIntentAsks is the retired needsCard("links") check re-pointed at the
// runtime owner-card path: on a permissive kernel with a live job table it
// denies the typed _xbot_links intent inside a real prepared Shell call and
// reports whether the card still went out, which no short-circuit may skip.
func linksIntentAsks(t *testing.T, action, id string) bool {
	t.Helper()
	k := &Kernel{Bot: testBot(t, link.NameTerminal), Permissive: true}
	k.ensureTurn().turnTrusted = true
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	defer k.CloseOperations()
	asked := false
	approve := func(context.Context, string, string) (bool, error) {
		asked = true
		return false, nil
	}
	// The bare context has no source correlation, which is what a call that
	// never passed through a prepared Shell call must see.
	if _, _, err := k.runtimeApprovalSource(context.Background()); err == nil {
		t.Error("bare context reached the source approval path")
	}
	endpoint := ""
	for _, item := range k.CLIEnvironment(nil) {
		if value, ok := strings.CutPrefix(item, "XBOT_ENDPOINT="); ok {
			endpoint = value
		}
	}
	runRoutineSource(t, k, approve, `{"command":"\"$XBOT_BIN\" links `+action+` --self `+id+`"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
		var reply cliops.LinksReply
		var err error
		switch action {
		case "install":
			reply, err = cliops.CallLinksInstall(context.Background(), endpoint, k.Bot.Root, tools.CallID(ctx), cliops.LinksInstall{ID: id})
		default:
			reply, err = cliops.CallLinksRemove(context.Background(), endpoint, k.Bot.Root, tools.CallID(ctx), cliops.LinksRemove{ID: id})
		}
		if err == nil || !strings.Contains(reply.Error, "denied by user") {
			t.Errorf("denied links %s reply=%+v err=%v", action, reply, err)
		}
		return "", nil
	})
	return asked
}

// With the table's cards off (permanent for serve/chat, ADR 0019),
// calls run without permission cards; the links install/remove owner card
// still goes out because secrets reach the bot only through it.
func TestPermissiveAsksOnlyWhereACardIsNeeded(t *testing.T) {
	k := &Kernel{Bot: testBot(t), Permissive: true}
	k.ensureTurn().turnTrusted = true
	checked := 0
	for _, tool := range []string{"mcp_x_send", "delete_file", "secret_rm"} {
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
	if !linksIntentAsks(t, "install", "telegram") {
		t.Error("the links install card was dropped")
	}
	// The retired restart_xbot keeps no table entry: it runs through the
	// runtime CLI, which never sends a card.
	if review.Known("restart_xbot") {
		t.Error("retired restart_xbot keeps a policy entry")
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

// The runtime restart path qualifies the original source Shell call, then
// calls k.Restart directly: no approver, no card, under every trust and
// permissive combination — the retired tool's original semantics.
func TestRestartRuntimeRunsWithoutCardEveryPolicy(t *testing.T) {
	k, _ := doorKernel(t, nil)
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	defer k.CloseOperations()
	endpoint := ""
	for _, item := range k.CLIEnvironment(nil) {
		if value, ok := strings.CutPrefix(item, "XBOT_ENDPOINT="); ok {
			endpoint = value
		}
	}
	for _, trusted := range []bool{false, true} {
		for _, permissive := range []bool{false, true} {
			k.ensureTurn().turnTrusted = trusted
			k.Permissive = permissive
			var hint string
			k.Restart = func(h string) error { hint = h; return nil }
			cards := 0
			approve := func(context.Context, string, string) (bool, error) {
				cards++
				return false, nil
			}
			runRoutineSource(t, k, approve, `{"command":"\"$XBOT_BIN\" restart --self"}`, func(ctx context.Context, _ json.RawMessage) (string, error) {
				reply, err := cliops.CallRestart(ctx, endpoint, k.Bot.Root, tools.CallID(ctx), cliops.RestartCall{Hint: "继续检查"})
				if err != nil || !strings.Contains(reply.Text, "重启") {
					t.Errorf("restart reply=%+v err=%v", reply, err)
				}
				return "", nil
			})
			if hint != "继续检查" || cards != 0 {
				t.Fatalf("trusted=%v permissive=%v: hint=%q cards=%d", trusted, permissive, hint, cards)
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
	k := &Kernel{Bot: testBot(t), Permissive: true}
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
	b := testBot(t)
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
		for _, tool := range []string{"delete_file", "mcp_x_send", "sh"} {
			if needsCard(t, k, tool, `{"method":"POST","command":"rm a.txt","text":"new persona"}`) {
				t.Fatalf("%s asked with permissions on=%v", tool, on)
			}
		}
		if !linksIntentAsks(t, "install", "telegram") {
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
