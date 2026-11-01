package kernel

import (
	"context"
	"encoding/json"
	"testing"
)

func TestBotControlResearchHelpOnly(t *testing.T) {
	k, _, _ := freshBot(t)
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	defer k.CloseOperations()
	interpreter := &boundReadOnlyInterpreter{}
	shell := readOnlyShell{Tool: interpreter, k: k}
	for _, command := range []string{`"$XBOT_BIN" pin --help`, `"$XBOT_BIN" unpin -h`, `"$XBOT_BIN" primary --help`} {
		raw, _ := json.Marshal(map[string]string{"command": command})
		if _, err := shell.Run(context.Background(), raw); err != nil {
			t.Fatalf("help rejected %s %v", command, err)
		}
	}
	for _, command := range []string{`"$XBOT_BIN" pin target --self`, `"$XBOT_BIN" unpin target --self`, `"$XBOT_BIN" primary set target --self`, `"$XBOT_BIN" primary`, `"$XBOT_BIN" pin target`, `"$XBOT_BIN" primary --help; echo injected`, `"$XBOT_BIN" --root /other pin --help`, `xbot primary --help`} {
		raw, _ := json.Marshal(map[string]string{"command": command})
		if _, err := shell.Run(context.Background(), raw); err == nil {
			t.Fatalf("write/offline/other source reached interpreter %s", command)
		}
	}
	if interpreter.calls != 3 {
		t.Fatalf("rejected actions reached interpreter: %d calls", interpreter.calls)
	}
}
