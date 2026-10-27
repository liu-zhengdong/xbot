package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Message bubbles render $…$ inline and $$…$$ block math through KaTeX; the
// TeX source itself must never enter the DOM as HTML (KaTeX's escaped output
// is the only innerHTML), currency-like "$5 和 $6" stays prose, code spans
// protect their $, and a missing or failing KaTeX falls back to the raw
// source instead of breaking the bubble. Runs the real app.js functions under
// node with stubs for the DOM, plus the real vendored KaTeX for parse-failure
// regressions: with throwOnError the library throws on bad TeX and the catch
// restores the source, instead of returning .katex-error markup (review R4).
func TestBubbleMathRendering(t *testing.T) {
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	katex, err := staticFS.ReadFile("static/vendor/katex/katex.min.js")
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
const fs = require("node:fs");
// The real vendored KaTeX, loaded the CommonJS way its UMD wrapper supports.
const realKatex = require(process.argv[2]);
assert.equal(typeof realKatex.renderToString, "function", "vendored katex did not load");
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
const realKatexStub = { renderToString: (tex, opts) => { calls.push([tex, !!(opts && opts.displayMode)]); return "<katex>" + tex + "</katex>"; } };
const window = { katex: realKatexStub };
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

// Bare URLs link like [text](url) ones; trailing punctuation stays prose.
const urlP = document.createElement("p");
appendInline(urlP, "看 https://diygod.cc/home-gateway/，和 https://a.b/c). 完");
const links = urlP.childNodes.filter((n) => n.tagName === "A");
assert.deepEqual(links.map((a) => [a.textContent, a.href, a.target]),
  [["https://diygod.cc/home-gateway/", "https://diygod.cc/home-gateway/", "_blank"], ["https://a.b/c", "https://a.b/c", "_blank"]]);
assert.ok(dump(urlP).includes('"). 完"'), "trailing punctuation eaten: " + dump(urlP));

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

// The first closing $$ ends the formula: a second formula on the same line
// must become its own math segment, not TeX of the first one (review R3).
calls.length = 0;
out = run(appendBlocks, "$$a$$ 和 $$b$$");
assert.equal((out.match(/math-(block|inline)/g) || []).length, 2, "two formulas expected: " + out);
assert.ok(out.includes(" 和 "), "prose between the formulas lost: " + out);
assert.deepEqual(calls, [["a", true], ["b", false]], "split calls wrong: " + JSON.stringify(calls));

// A multi-line block whose closing line carries more math must close at the
// first $$ and keep going with the tail (review R3).
calls.length = 0;
out = run(appendBlocks, "$$\nx=1\n$$ 后面 $$y$$");
assert.equal((out.match(/math-(block|inline)/g) || []).length, 2, "tail formula swallowed: " + out);
assert.ok(out.includes("后面"), "prose on the closing line lost: " + out);
assert.deepEqual(calls, [["x=1", true], ["y", false]], "tail split calls wrong: " + JSON.stringify(calls));

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
// A sentence-middle $$…$$ is rendered inline but keeps its $$ delimiters in
// the fallback — displayMode and the source delimiters are separate (R5).
fallback = mathNode("x+y", false, "$$x+y$$");
assert.equal(fallback.textContent, "$$x+y$$", "sentence $$ fallback lost its delimiters");
window.katex = { renderToString: () => { throw Error("ParseError: bad"); } };
fallback = mathNode("\\frac{", true);
assert.deepEqual(fallback.classList._cls, ["math-raw"], "throwing katex must mark raw");
assert.equal(fallback.textContent, "$$\\frac{$$", "fallback must show the block source");
window.katex = { renderToString: (tex) => "<b>escaped " + tex + "</b>" };
const box = mathNode("<img src=x onerror=alert(1)>", false);
assert.equal(box.innerHTML, "<b>escaped <img src=x onerror=alert(1)></b>", "katex output must pass through as-is");

// Real vendored KaTeX, illegal TeX: renderToString must throw into the catch
// and the full path must fall back to the source words, with no .katex-error
// markup anywhere (review R4 — throwOnError:false used to defeat the catch).
// The stub's classList/textContent/innerHTML live on plain properties, so the
// nodes are inspected directly instead of through dump().
window.katex = realKatex;
calls.length = 0;
const badP = document.createElement("div");
appendInline(badP, "坏 $\\frac{$ 结束");
const rawSpan = badP.childNodes.find((n) => n.nodeType === 1 && (n.classList._cls || []).includes("math-raw"));
assert.ok(rawSpan, "real parse failure did not fall back: " + badP.childNodes.map(dump).join(","));
assert.equal(rawSpan.textContent, "$\\frac{$", "fallback lost the source: " + rawSpan.textContent);
assert.ok(!String(rawSpan.innerHTML || "").includes("katex-error"), ".katex-error markup leaked");
assert.ok(badP.childNodes.some((n) => n.nodeType === 3 && n.nodeValue.trim() === "结束"), "prose after bad math lost");
const badBlock = document.createElement("div");
appendBlocks(badBlock, "坏\n$$\\frac{$$\n结束");
const rawBlock = badBlock.childNodes.find((n) => n.nodeType === 1 && (n.classList._cls || []).includes("math-raw"));
assert.ok(rawBlock && rawBlock.tagName === "DIV", "real block parse failure did not fall back");
assert.equal(rawBlock.textContent, "$$\\frac{$$", "block fallback lost the source: " + rawBlock.textContent);
// Valid math still renders through the real library.
const goodP = document.createElement("div");
appendInline(goodP, "好 $E=mc^2$ 好");
const good = goodP.childNodes.find((n) => n.nodeType === 1 && n.className === "math-inline");
assert.ok(good && String(good.innerHTML).includes("katex"), "valid math did not render through the real katex");
assert.ok(!(good.classList._cls || []).includes("math-raw"), "valid math fell back");
`, "@BQ@", "`")
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "math_rendering.js")
	katexPath := filepath.Join(dir, "katex.min.js")
	if err := os.WriteFile(scriptPath, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(katexPath, katex, 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("node", scriptPath, katexPath).CombinedOutput(); err != nil {
		t.Fatalf("bubble math rendering: %v\n%s", err, out)
	}
}
