package web

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestChatEnterRespectsIME(t *testing.T) {
	for _, tc := range []struct {
		name, file, target, indent string
		multiline                  bool
	}{
		{"composer", "app.js", "input", "  ", true},
		{"other-option", "app.js", "field", "    ", false},
		{"remotechat", "remotechat.js", "el.input", "    ", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, err := staticFS.ReadFile("static/" + tc.file)
			if err != nil {
				t.Fatal(err)
			}
			start := tc.target + `.addEventListener("keydown", (ev) => {`
			_, body, ok := strings.Cut(string(source), start)
			if !ok {
				t.Fatal("keydown handler not found")
			}
			body, _, ok = strings.Cut(body, "\n"+tc.indent+"});")
			if !ok {
				t.Fatal("keydown handler end not found")
			}
			encoded, _ := json.Marshal(body)
			script := `
const assert = require("node:assert/strict");
let sent = 0;
const send = () => sent++;
const handler = new Function("ev", "submit", "composer", "sendText", "el", ` + string(encoded) + `);
function press(values) {
  let prevented = false;
  const ev = {key:"Enter", keyCode:13, shiftKey:false, isComposing:false,
    ...values, preventDefault(){prevented = true}};
  const before = sent;
  handler(ev, send, {requestSubmit:send}, send, {input:{value:"中文"}});
  return [sent - before, prevented];
}
assert.deepEqual(press({isComposing:true}), [0,false], "IME Enter must select, not send");
assert.deepEqual(press({keyCode:229}), [0,false], "229 fallback must not send");
assert.deepEqual(press({}), [1,true], "Enter after composition must send once");
assert.deepEqual(press({key:"a", keyCode:65}), [0,false], "other keys must keep their default");
`
			if tc.multiline {
				script += `assert.deepEqual(press({shiftKey:true}), [0,false], "Shift+Enter must retain newline");`
			}
			if output, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
				t.Fatalf("Enter behavior: %v\n%s", err, output)
			}
		})
	}
}
