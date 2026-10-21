package web

import (
	"os/exec"
	"strings"
	"testing"
)

func TestBackgroundEventBehavior(t *testing.T) {
	js, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	source := string(js)
	extract := func(start, end string) string {
		t.Helper()
		a := strings.Index(source, "  function "+start+"(")
		if a < 0 {
			t.Fatalf("missing %s", start)
		}
		b := strings.Index(source[a:], "  function "+end+"(")
		if b < 0 {
			t.Fatalf("missing end %s", end)
		}
		return source[a : a+b]
	}
	script := extract("unwrap", "displayText") + extract("displayText", "routineOf") + `
const assert = require("node:assert/strict");
const assistant = (content, branch, tool_calls) => ({role:"assistant", content, branch, tool_calls});
const event = (text, type="fork_end", status="") => ({role:"user", content:
 '<untrusted_event source="subagent" type="'+type+'" status="'+status+'">\n'+text+'\n</untrusted_event>'});
const check = (messages, expected) => assert.deepEqual([...backgroundRelays(messages)], expected);
check([assistant("same"), event("same"), assistant("same"), assistant("same")], [2]);
check([event("same"), assistant("different"), assistant("same")], []);
check([event("same"), assistant("", null, [{}]), assistant("same")], []);
check([assistant("intermediate", "bg_a"), assistant("intermediate"), assistant("final", "bg_a"), assistant("final"), assistant("final")], [3]);
check([assistant("same"), assistant("same", "bg_a")], []);
check([assistant("one", "bg_a"), assistant("two", "bg_b"), assistant("one"), assistant("two")], [2]);
check([event("a \n b"), assistant(" a b ")], [1]);
check([event("![pic](one.png)"), assistant("![pic](two.png)")], []);
check([event("[file](one.pdf)"), assistant("[file](two.pdf)")], []);
check([event("![pic](one.png)"), assistant("![pic](one.png)")], [1]);
check([event("bg_abcd same"), assistant("same")], []);
check([event("same", "done"), assistant("same")], []);
const openRoutines = new Set();
const clip = s => s;
const plainText = s => s;
const icon = () => ({});
const el = (tag, cls, text) => ({text, hidden:false, children:[], append(...xs){this.children.push(...xs)}, setAttribute(){}, addEventListener(){}});
const label = (text, type, status) => eventLine({text, type, source:"subagent", attributes:{status}}, "key").children[0].children[1].text;
assert.equal(label("provider: request timeout after 3m0s", "done", "done"), "子任务：provider: request timeout after 3m0s");
assert.equal(label("error discussed", "done", "done"), "子任务：error discussed");
for (const n of [1, 3, 12]) assert.equal(label("provider: request timeout after "+n+"m0s", "fork_end", "failed"), "子任务失败：模型超时（"+n+" 分钟）");
assert.equal(label("request timeout after 9m", "error", ""), "子任务失败：模型超时（9 分钟）");
assert.equal(label("other reason", "failed", ""), "子任务失败：出错了");
`
	cmd := exec.Command("node", "-e", script)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("JavaScript regression: %v\n%s", err, output)
	}
}
