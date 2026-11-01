package review

import "testing"

func TestDirectXBotArgsPreservesLiteralArguments(t *testing.T) {
	for _, command := range []string{`"$XBOT_BIN" mcp call docs query --self --readonly --args '{"q":"a|b;$(literal)","n":9007199254740993}'`, `'/runtime/xbot' mcp tools docs --self`} {
		args, ok := DirectXBotArgs(command, "/runtime/xbot")
		if !ok || len(args) < 3 || args[0] != "mcp" {
			t.Fatalf("literal rejected: %q %v", command, args)
		}
	}
	for _, command := range []string{`xbot mcp list --self`, `"$XBOT_BIN" mcp list --self; touch x`, `"$XBOT_BIN" mcp list --self | cat`, `env XBOT_BIN=/other "$XBOT_BIN" mcp list --self`, `"$XBOT_BIN" mcp list --self > x`, `"$XBOT_BIN" mcp tools "$SERVER" --self`, `"$XBOT_BIN" mcp tools d"ocs" --self`, `"$XBOT_BIN" mcp tools $(echo docs) --self`, `"$XBOT_BIN" mcp list --self` + "\n" + `echo x`, `"/other/xbot" mcp list --self`} {
		if args, ok := DirectXBotArgs(command, "/runtime/xbot"); ok {
			t.Fatalf("expanded/composed command accepted: %q %v", command, args)
		}
	}
}
