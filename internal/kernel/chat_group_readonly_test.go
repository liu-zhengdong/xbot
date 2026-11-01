package kernel

import (
	"context"
	"encoding/json"
	"testing"
)

func TestChatGroupRuntimeResearchReadOnly(t *testing.T) {
	k, _, _ := freshBot(t)
	if err := k.StartOperations(); err != nil {
		t.Fatal(err)
	}
	defer k.CloseOperations()
	interpreter := &boundReadOnlyInterpreter{}
	shell := readOnlyShell{Tool: interpreter, k: k}
	allowed := []string{
		`"$XBOT_BIN" chat-group list --self --json`,
		`"$XBOT_BIN" chat-group history g_12345678 --self --limit 3 --before-id gm_a --sender B --keyword needle`,
		`"$XBOT_BIN" chat-group history g_12345678 --self --keyword --help`,
		`"$XBOT_BIN" chat-group --help`,
		`"$XBOT_BIN" chat-group create --help`,
		`"$XBOT_BIN" chat-group post --help`,
	}
	for _, command := range allowed {
		raw, _ := json.Marshal(map[string]string{"command": command})
		if _, err := shell.Run(context.Background(), raw); err != nil {
			t.Fatalf("read/help rejected %s: %v", command, err)
		}
	}
	for _, command := range []string{
		`"$XBOT_BIN" chat-group list`,
		`"$XBOT_BIN" chat-group history g_12345678`,
		`"$XBOT_BIN" chat-group create room --self --member B`,
		`"$XBOT_BIN" chat-group post g_12345678 --self --text body`,
		`"$XBOT_BIN" chat-group post g_12345678 --self --file body.txt`,
		`"$XBOT_BIN" chat-group post g_12345678 --self --file -`,
		`"$XBOT_BIN" chat-group post g_12345678 --self --text --help`,
		`"$XBOT_BIN" chat-group create room --self --member --help`,
		`"$XBOT_BIN" chat-group create --self --member B -- --help`,
		`"$XBOT_BIN" chat-group list --self; echo injected`,
		`"$XBOT_BIN" --root /other chat-group list --self`,
		`xbot chat-group list --self`,
		`"$XBOT_BIN" group list`,
	} {
		raw, _ := json.Marshal(map[string]string{"command": command})
		if _, err := shell.Run(context.Background(), raw); err == nil {
			t.Fatalf("write/foreign source reached interpreter: %s", command)
		}
	}
	if interpreter.calls != len(allowed) {
		t.Fatalf("rejected commands reached interpreter: %d calls, want %d", interpreter.calls, len(allowed))
	}
}
