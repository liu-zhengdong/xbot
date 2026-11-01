package kernel

import (
	"context"
	"encoding/json"
	"testing"
)

type boundReadOnlyInterpreter struct{ calls int }

func (*boundReadOnlyInterpreter) Name() string        { return "sh" }
func (*boundReadOnlyInterpreter) Description() string { return "fixture interpreter" }
func (*boundReadOnlyInterpreter) Parameters() any     { return map[string]any{"type": "object"} }
func (*boundReadOnlyInterpreter) Confirm(json.RawMessage) (bool, string, error) {
	return false, "", nil
}
func (s *boundReadOnlyInterpreter) Run(context.Context, json.RawMessage) (string, error) {
	s.calls++
	return "accepted", nil
}

func TestResearchRoutineDirectCommandsUseBoundSource(t *testing.T) {
	k, _, _ := freshBot(t)
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	defer k.CloseOperations()
	interpreter := &boundReadOnlyInterpreter{}
	shell := readOnlyShell{Tool: interpreter, k: k}
	for _, command := range []string{`"$XBOT_BIN" routine list --self --json`, `"$XBOT_BIN" routine --help`} {
		raw, _ := json.Marshal(map[string]string{"command": command})
		if _, err := shell.Run(context.Background(), raw); err != nil {
			t.Fatalf("read rejected %s: %v", command, err)
		}
	}
	for _, command := range []string{
		`"$XBOT_BIN" routine create --self --args '{}'`,
		`"$XBOT_BIN" routine update r --self --args '{}'`,
		`"$XBOT_BIN" routine delete r --self`,
		`"$XBOT_BIN" routine list --bot other`,
		`"$XBOT_BIN" routine migrate --dry-run`,
		`"$XBOT_BIN" --root /other routine list --self`,
		`"$XBOT_BIN" routine list --self; echo injected`,
		`xbot routine list --self`,
	} {
		raw, _ := json.Marshal(map[string]string{"command": command})
		if _, err := shell.Run(context.Background(), raw); err == nil {
			t.Fatalf("write/other source accepted %s", command)
		}
	}
	if interpreter.calls != 2 {
		t.Fatalf("rejected command reached interpreter: %d", interpreter.calls)
	}
}

func TestResearchSkillDirectCommandsUseBoundSource(t *testing.T) {
	k, _, _ := freshBot(t)
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	defer k.CloseOperations()
	interpreter := &boundReadOnlyInterpreter{}
	shell := readOnlyShell{Tool: interpreter, k: k}
	for _, command := range []string{`"$XBOT_BIN" skill read ship --self --json`, `"$XBOT_BIN" skill --help`} {
		raw, _ := json.Marshal(map[string]string{"command": command})
		if _, err := shell.Run(context.Background(), raw); err != nil {
			t.Fatalf("read rejected %s: %v", command, err)
		}
	}
	for _, command := range []string{
		`"$XBOT_BIN" skill write ship --description d --body body --self`,
		`"$XBOT_BIN" skill show ship`,
		`"$XBOT_BIN" --root /other skill read ship --self`,
		`"$XBOT_BIN" skill read ship --self; echo injected`,
		`xbot skill read ship --self`,
	} {
		raw, _ := json.Marshal(map[string]string{"command": command})
		if _, err := shell.Run(context.Background(), raw); err == nil {
			t.Fatalf("write/other-target accepted %s", command)
		}
	}
	if interpreter.calls != 2 {
		t.Fatalf("rejected command reached interpreter before any CLI RPC: calls=%d", interpreter.calls)
	}
}

func TestResearchMCPDirectCommandsUseBoundSource(t *testing.T) {
	k, _, _ := freshBot(t)
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	defer k.CloseOperations()
	interpreter := &boundReadOnlyInterpreter{}
	shell := readOnlyShell{Tool: interpreter, k: k}
	for _, command := range []string{
		`"$XBOT_BIN" mcp list --self --json`,
		`"$XBOT_BIN" mcp tools docs --self`,
		`"$XBOT_BIN" mcp describe docs 'remote/name' --self`,
		`"$XBOT_BIN" mcp call docs query --self --readonly --args '{"n":9007199254740993}'`,
	} {
		raw, _ := json.Marshal(map[string]string{"command": command})
		if _, err := shell.Run(context.Background(), raw); err != nil {
			t.Fatalf("read rejected %s: %v", command, err)
		}
	}
	if interpreter.calls != 4 {
		t.Fatal("read did not reach original interpreter")
	}
	for _, command := range []string{
		`"$XBOT_BIN" mcp call docs query --self`,
		`"$XBOT_BIN" mcp add docs --url http://127.0.0.1:9 --self`,
		`"$XBOT_BIN" mcp list --http 127.0.0.1:9 --bot other`,
		`"$XBOT_BIN" mcp offline list other`,
		`"$XBOT_BIN" --root /other mcp list --self`,
		`"$XBOT_BIN" mcp list --self; echo injected`,
		`xbot mcp list --self`,
	} {
		raw, _ := json.Marshal(map[string]string{"command": command})
		if _, err := shell.Run(context.Background(), raw); err == nil {
			t.Fatalf("write/other-target accepted %s", command)
		}
	}
	if interpreter.calls != 4 {
		t.Fatal("rejected command reached interpreter")
	}
}
