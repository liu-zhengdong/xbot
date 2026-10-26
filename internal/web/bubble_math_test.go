package web

import (
	"os/exec"
	"strings"
	"testing"
)

// Message bubbles render $…$ inline and $$…$$ block math through KaTeX; the
// TeX source itself must never enter the DOM as HTML (KaTeX's escaped output
// is the only innerHTML), currency-like "$5 和 $6" stays prose, code spans
// protect their $, and a missing or failing KaTeX falls back to the raw
// source instead of breaking the bubble. Runs the real app.js functions under
// node with stubs for the DOM and window.katex.
func TestBubbleMathRendering(t *testing.T) {
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	a := strings.Index(s, "  function mathNode(")
	b := strings.Index(s, "  function safeImage(")
	if a < 0 || b <= a {
		t.Fatal("math/markdown block not found in app.js")
	}
	script := strings.ReplaceAll(`
const assert = require("node:assert/strict");
const document = {
  createTextNode: (v) => ({ nodeType: 3, nodeValue: String(v) }),
  createElement: (tag) => ({
    nodeType: 1, tagName: String(tag).toUpperCase(), childNodes: [], style: {}, className: "",
    append(...kids) { this.childNodes.push(...kids); },
    classList: { add(c) { (this._cls = this._cls || []).push(c); } },
  }),
};
const el = (tag, cls, text) => { const n = document.createElement(tag); n.className = cls || ""; if (text != null) n.textContent = String(text); n.addEventListener = () => {}; return n; };
const pathLabel = (path, tag) => el(tag || "span", "compact-path", path);
const copyText = () => {};
const calls = [];
const realKatex = { renderToString: (tex, opts) => { calls.push([tex, !!(opts && opts.displayMode)]); return "<katex>" + tex + "</katex>"; } };
const window = { katex: realKatex };
`+s[a:b]+`
const dump = (n) => n.nodeType === 3 ? JSON.stringify(n.nodeValue)
  : n.tagName + (n.className ? "." + n.className : "") + "[" + (n.childNodes || []).map(dump).join(",") + "]";
const run = (fn, ...rest) => { const p = document.createElement("div"); fn(p, ...rest); return dump(p); };

// Inline math: the $ pair goes away, the formula reaches KaTeX, the prose stays.
calls.length = 0;
let out = run(appendInline, "质能式 $E=mc^2$ 很有名");
assert.ok(out.includes("SPAN.math-inline"), "no inline math span: " + out);
assert.ok(out.includes("有名"), "prose around math lost");
assert.deepEqual(calls, [["E=mc^2", false]], "katex call wrong: " + JSON.stringify(calls));

// Currency is not math: the closing side is a space.
out = run(appendInline, "价格 $5 和 $6 元");
assert.ok(!out.includes("math"), "currency became math: " + out);
assert.ok(out.includes("价格 $5 和 $6 元"), "currency text changed: " + out);

// Two formulas in one line; $$ glued into a sentence stays an inline span.
out = run(appendInline, "$a$ 和 $b$");
assert.equal((out.match(/math-inline/g) || []).length, 2, "two formulas expected: " + out);
out = run(appendInline, "看 $$x+y$$ 看");
assert.ok(out.includes("SPAN.math-inline"), "inline $$ should stay a span: " + out);

// Code spans protect their dollars.
out = run(appendInline, "@BQ@$x$@BQ@ 是源码");
assert.ok(out.includes("CODE") && !out.includes("math"), "code span did not protect $: " + out);

// Block math on one line: display mode, its own div.
calls.length = 0;
out = run(appendBlocks, "$$\\frac{1}{2}$$");
assert.ok(out.includes("DIV.math-block"), "no display block: " + out);
assert.deepEqual(calls, [["\\frac{1}{2}", true]], "display call wrong: " + JSON.stringify(calls));

// Multi-line $$ blocks keep surrounding prose, and prose after the closing
// $$ on the same line goes on as text.
out = run(appendBlocks, "前文\n$$\nx = 1\ny = 2\n$$\n后文");
assert.ok(out.includes("前文") && out.includes("后文"), "prose around block lost: " + out);
assert.deepEqual(calls[calls.length - 1], ["x = 1\ny = 2", true], "multiline tex wrong: " + JSON.stringify(calls[calls.length - 1]));
out = run(appendBlocks, "$$x^2$$ 之后");
assert.ok(out.startsWith("DIV[DIV.math-block") && out.includes("之后"), "prose after closing $$ lost: " + out);

// Headings and lists still work next to math.
out = run(appendBlocks, "# 标\n- $x^2$ 项");
assert.ok(out.includes("SPAN.math-inline") && out.includes("项") && out.includes("标"), "mixed blocks broke: " + out);

// A stream that has not seen the closing $$ yet renders what it has (the
// whole-segment re-render makes it correct once the pair completes).
out = run(appendBlocks, "开始\n$$\nx = 1");
assert.ok(out.includes("DIV.math-block"), "unclosed $$ swallowed everything: " + out);

// The full path: code fences stay untouched, math around them renders.
out = run(appendMarkdown, "看 $a$\n@BQ@@BQ@@BQ@go\nx := 1\n@BQ@@BQ@@BQ@\n再 $b$");
assert.equal((out.match(/SPAN.math-inline/g) || []).length, 2, "both formulas expected: " + out);
assert.ok(out.includes("code-block") && out.includes("x"), "code fence broke: " + out);

// Fallbacks: missing KaTeX or a parse error leave the raw source, never an
// exception, and only the library's output is ever handed to innerHTML.
window.katex = null;
let fallback = mathNode("E=mc^2", false);
assert.deepEqual(fallback.classList._cls, ["math-raw"], "missing katex must mark raw: " + JSON.stringify(fallback._cls));
assert.equal(fallback.textContent, "$E=mc^2$", "fallback must show the source");
window.katex = { renderToString: () => { throw Error("ParseError: bad"); } };
fallback = mathNode("\\frac{", true);
assert.deepEqual(fallback.classList._cls, ["math-raw"], "throwing katex must mark raw");
assert.equal(fallback.textContent, "$$\\frac{$$", "fallback must show the block source");
window.katex = { renderToString: (tex) => "<b>escaped " + tex + "</b>" };
const box = mathNode("<img src=x onerror=alert(1)>", false);
assert.equal(box.innerHTML, "<b>escaped <img src=x onerror=alert(1)></b>", "katex output must pass through as-is");
`, "@BQ@", "`")
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("bubble math rendering: %v\n%s", err, out)
	}
}
