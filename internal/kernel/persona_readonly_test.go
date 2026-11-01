package kernel

import (
	"context"
	"encoding/json"
	"testing"
)

func TestPersonaResearchHelpOnly(t *testing.T) {
	k, _, _ := freshBot(t)
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	defer k.CloseOperations()
	interpreter := &boundReadOnlyInterpreter{}
	shell := readOnlyShell{Tool: interpreter, k: k}
	for _, command := range []string{`"$XBOT_BIN" persona --help`, `"$XBOT_BIN" persona set --help`} {
		raw, _ := json.Marshal(map[string]string{"command": command})
		if _, err := shell.Run(context.Background(), raw); err != nil {
			t.Fatalf("help rejected %s %v", command, err)
		}
	}
	for _, command := range []string{`"$XBOT_BIN" persona set --self --text duties`, `"$XBOT_BIN" persona set --self --file duties.txt`, `"$XBOT_BIN" persona set --self --file -`, `"$XBOT_BIN" persona set --text duties`, `"$XBOT_BIN" persona set --self --file --help`, `"$XBOT_BIN" persona set --self --text --help`, `"$XBOT_BIN" persona --help; echo injected`, `"$XBOT_BIN" --root /other persona --help`, `xbot persona --help`} {
		raw, _ := json.Marshal(map[string]string{"command": command})
		if _, err := shell.Run(context.Background(), raw); err == nil {
			t.Fatalf("write/flagvalue/other source reached interpreter %s", command)
		}
	}
	if interpreter.calls != 2 {
		t.Fatalf("rejected actions reached interpreter: %d calls", interpreter.calls)
	}
}
