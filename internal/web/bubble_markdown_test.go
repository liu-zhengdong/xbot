package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Message bubbles render fenced code with per-language token spans, > quotes
// as nested blockquotes with no > left in the text, and GFM pipe tables as
// real tables whose cells go through the inline path (http(s) links only,
// everything else as text). Runs the real app.js renderer under node with
// DOM stubs that record only textContent, so any markup in the source would
// show up as literal text rather than elements.
func TestBubbleMarkdownRendering(t *testing.T) {
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	a := strings.Index(s, "  function mathNode(")
	b := strings.Index(s, "  function safeImage(")
	if a < 0 || b <= a {
		t.Fatal("markdown block not found in app.js")
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
const window = { katex: { renderToString: (tex, opts) => { calls.push([tex, !!(opts && opts.displayMode)]); return "<katex>" + tex + "</katex>"; } } };
`+s[a:b]+`
const dump = (n) => n.nodeType === 3 ? JSON.stringify(n.nodeValue)
  : n.tagName + (n.className ? "." + n.className : "") + (n.textContent != null ? "{" + JSON.stringify(n.textContent) + "}" : "") + "[" + (n.childNodes || []).map(dump).join(",") + "]";
const text = (n) => n.nodeType === 3 ? n.nodeValue : (n.textContent != null ? n.textContent : "") + (n.childNodes || []).map(text).join("");
const find = (n, pred, out = []) => { if (pred(n)) out.push(n); for (const k of n.childNodes || []) find(k, pred, out); return out; };
const render = (fn, src) => { const p = document.createElement("div"); fn(p, src); return p; };
const tokens = (root, cls) => find(root, (n) => n.className === cls).map((n) => n.textContent);

// Python: def/return are keywords, strings, comments and numbers colored, and
// the code text survives byte for byte.
const py = "def add(a, b):\n    # sum\n    return a + b + 1  # 'x'\nprint(\"hi\")\n'''doc'''";
let root = render(appendMarkdown, "@BQ@@BQ@@BQ@python\n" + py + "\n@BQ@@BQ@@BQ@");
const code = find(root, (n) => n.tagName === "CODE")[0];
assert.equal(text(code), py, "code text changed");
assert.deepEqual(tokens(code, "kw"), ["def", "return"], "python keywords: " + dump(code));
assert.deepEqual(tokens(code, "cm"), ["# sum", "# 'x'"], "python comments: " + dump(code));
assert.deepEqual(tokens(code, "str"), ['"hi"', "'''doc'''"], "python strings: " + dump(code));
assert.deepEqual(tokens(code, "num"), ["1"], "python numbers: " + dump(code));
assert.ok(tokens(code, "fn").includes("add"), "function names: " + dump(code));

// JS/TS/Go keep working; unknown or missing language stays plain text.
for (const [lang, src, kw] of [["js", "const x = 'a'; // c", "const"], ["ts", "interface A {}", "interface"], ["go", "func main() { return }", "func"], ["bash", "echo $HOME # c", null]]) {
  const r = render(appendMarkdown, "@BQ@@BQ@@BQ@" + lang + "\n" + src + "\n@BQ@@BQ@@BQ@");
  const c = find(r, (n) => n.tagName === "CODE")[0];
  assert.equal(text(c), src, lang + " text changed");
  if (kw) assert.ok(tokens(c, "kw").includes(kw), lang + " keyword missing: " + dump(c));
  assert.ok(find(c, (n) => n.nodeType === 1).length > 0, lang + " got no tokens: " + dump(c));
}
// Shell $# is not a comment; a word-leading # is.
let sh = find(render(appendMarkdown, "@BQ@@BQ@@BQ@sh\necho $# # n\n@BQ@@BQ@@BQ@"), (n) => n.tagName === "CODE")[0];
assert.deepEqual(tokens(sh, "cm"), ["# n"], "shell comment: " + dump(sh));
let plain = find(render(appendMarkdown, "@BQ@@BQ@@BQ@\nif x: return 1\n@BQ@@BQ@@BQ@"), (n) => n.tagName === "CODE")[0];
assert.equal(plain.textContent, "if x: return 1", "untagged code must stay plain");
// Markup inside code is text, and an unterminated quote does not swallow the rest.
const evil = "x = '<img src=x onerror=alert(1)>'\ny = \"open\nreturn 2";
const ec = find(render(appendMarkdown, "@BQ@@BQ@@BQ@py\n" + evil + "\n@BQ@@BQ@@BQ@"), (n) => n.tagName === "CODE")[0];
assert.equal(text(ec), evil, "escaped code text changed");
assert.ok(!find(ec, (n) => n.tagName === "IMG").length && !find(ec, (n) => "innerHTML" in n).length, "code produced markup");
assert.ok(tokens(ec, "kw").includes("return"), "unterminated quote swallowed later lines: " + dump(ec));

// Quotes: > and >> become nested blockquotes, no > reaches the text, and
// lists, inline marks and math work inside.
root = render(appendMarkdown, "前\n> 一层 **粗** $x^2$\n> - 项\n>> 二层 $$y$$\n> 回到一层\n后");
const quotes = find(root, (n) => n.tagName === "BLOCKQUOTE");
assert.equal(quotes.length, 2, "two quote levels expected: " + dump(root));
assert.ok(find(quotes[0], (n) => n === quotes[1]).length === 1, "inner quote not nested: " + dump(root));
assert.ok(!text(root).includes(">"), "> leaked into text: " + JSON.stringify(text(root)));
assert.ok(find(quotes[0], (n) => n.tagName === "STRONG").length === 1, "bold in quote lost");
assert.ok(find(quotes[0], (n) => n.tagName === "LI").length === 1, "list in quote lost");
assert.equal(find(quotes[0], (n) => /^math-/.test(n.className)).length, 2, "math in quote lost: " + dump(root));
assert.equal(find(quotes[1], (n) => /^math-/.test(n.className)).length, 1, "math in nested quote lost");
assert.ok(text(root).startsWith("前") && text(root).endsWith("后"), "prose around quote lost");
// Block math spanning quoted lines still closes inside the quote.
root = render(appendMarkdown, "> $$\n> a+b\n> $$\n> 完");
assert.equal(find(root, (n) => n.className === "math-block").length, 1, "quoted block math: " + dump(root));

// Tables: header + |---| + rows become thead/tbody; alignment, inline marks
// and http(s) links in cells; javascript: links stay text; \| and |`+"`a|b`"+`
// do not split cells.
root = render(appendMarkdown, "表：\n| 名 | 链接 | 值 |\n|:---|:---:|---:|\n| **a** | [x](https://e.com) | 1 |\n| b \\| c | [y](javascript:alert(1)) | @BQ@p|q@BQ@ |\n\n尾");
const tables = find(root, (n) => n.tagName === "TABLE");
assert.equal(tables.length, 1, "no table: " + dump(root));
const th = find(tables[0], (n) => n.tagName === "TH"), td = find(tables[0], (n) => n.tagName === "TD");
assert.deepEqual(th.map(text), ["名", "链接", "值"], "header cells: " + dump(tables[0]));
assert.equal(td.length, 6, "body cells: " + dump(tables[0]));
assert.deepEqual(th.map((n) => n.style.textAlign || ""), ["left", "center", "right"], "alignments");
const links = find(tables[0], (n) => n.tagName === "A");
assert.equal(links.length, 1, "only the https link may be a link: " + dump(tables[0]));
assert.equal(links[0].href, "https://e.com", "link href");
assert.equal(text(td[3]), "b | c", "escaped pipe split the cell");
assert.equal(text(td[5]), "p|q", "pipe in code split the cell");
assert.ok(text(td[4]).includes("javascript:"), "bad link must stay text");
assert.ok(!text(root).includes("---"), "separator row leaked");
assert.ok(text(root).startsWith("表：") && text(root).endsWith("尾"), "prose around table lost");
// A pipe line without a separator row stays prose (and a streaming header
// that has not got its separator yet does not break the bubble).
root = render(appendMarkdown, "a | b\n不是表");
assert.ok(!find(root, (n) => n.tagName === "TABLE").length, "pipe prose became a table");
root = render(appendMarkdown, "| a | b |");
assert.ok(text(root).includes("| a | b |"), "partial table lost");
// A fence inside a quote is parsed after the > prefix is gone: one quote,
// the code block inside it, highlighted, with no > in the code (review B1).
for (const pre of [">", ">>"]) {
  root = render(appendMarkdown, pre + " 引用\n" + pre + " @BQ@@BQ@@BQ@python\n" + pre + " def f():\n" + pre + "     return \"s\"\n" + pre + " @BQ@@BQ@@BQ@\n" + pre + " 结尾");
  const outer = root.childNodes;
  assert.equal(outer.length, 1, pre + " quote split around its fence: " + dump(root));
  assert.equal(outer[0].tagName, "BLOCKQUOTE", pre + " no quote: " + dump(root));
  const qc = find(outer[0], (n) => n.tagName === "CODE");
  assert.equal(qc.length, 1, pre + " code not inside the quote: " + dump(root));
  assert.equal(text(qc[0]), "def f():\n    return \"s\"", pre + " quoted code text: " + JSON.stringify(text(qc[0])));
  assert.deepEqual(tokens(qc[0], "kw"), ["def", "return"], pre + " quoted code lost highlighting");
  assert.ok(!text(root).includes(">"), pre + " > leaked: " + JSON.stringify(text(root)));
  assert.ok(text(root).startsWith("引用") && text(root).endsWith("结尾"), pre + " quote prose lost");
}
// A > line inside an ordinary fence is code, not a quote.
root = render(appendMarkdown, "@BQ@@BQ@@BQ@sh\necho a > out.txt\n> not a quote\n@BQ@@BQ@@BQ@");
assert.ok(!find(root, (n) => n.tagName === "BLOCKQUOTE").length, "> inside a fence became a quote: " + dump(root));
assert.equal(text(find(root, (n) => n.tagName === "CODE")[0]), "echo a > out.txt\n> not a quote", "fenced > lines changed");
// A quote line right after a table ends the table, pipes and all (review
// B2), through the full path and through appendBlocks alone.
for (const [pre, fn] of [[">", appendMarkdown], [">>", appendMarkdown], [">", appendBlocks], [">>", appendBlocks]]) {
  root = render(fn, "| a | b |\n|---|---|\n| 1 | 2 |\n" + pre + " x | y");
  const tb = find(root, (n) => n.tagName === "TABLE");
  assert.equal(tb.length, 1, pre + " table missing: " + dump(root));
  assert.equal(find(tb[0], (n) => n.tagName === "TD").length, 2, pre + " quote line swallowed into the table: " + dump(root));
  const bq = find(root, (n) => n.tagName === "BLOCKQUOTE");
  assert.ok(bq.length >= 1 && !find(tb[0], (n) => n.tagName === "BLOCKQUOTE").length, pre + " no quote after the table: " + dump(root));
  assert.equal(text(bq[0]), "x | y", pre + " quote text: " + JSON.stringify(text(bq[0])));
  assert.ok(!text(root).includes(">"), pre + " > leaked: " + JSON.stringify(text(root)));
}
// A leading > inside a $$ block is TeX, not a quote: one display formula
// gets the whole source, at top level and inside a quote, and a closing
// line that starts with > still belongs to the formula (review B3).
for (const [src, tex, quoted] of [
  ["$$\nx\n> 0\n$$", "x\n> 0", 0],
  ["> $$\n> x\n> > 0\n> $$", "x\n> 0", 1],
  ["$$\nx\n> 0 $$\n尾", "x\n> 0 ", 0],
  [">> $$\n>> x\n>> > 0\n>> $$", "x\n> 0", 2],
]) {
  calls.length = 0;
  root = render(appendMarkdown, src);
  assert.deepEqual(calls, [[tex, true]], JSON.stringify(src) + " katex calls: " + JSON.stringify(calls));
  assert.equal(find(root, (n) => n.className === "math-block").length, 1, JSON.stringify(src) + " math split: " + dump(root));
  assert.equal(find(root, (n) => n.tagName === "BLOCKQUOTE").length, quoted, JSON.stringify(src) + " quote count: " + dump(root));
}
// After the formula closes, > lines are quotes again.
calls.length = 0;
root = render(appendMarkdown, "$$\nx\n> 0\n$$\n> 引用");
assert.deepEqual(calls, [["x\n> 0", true]], "formula before quote: " + JSON.stringify(calls));
assert.equal(find(root, (n) => n.tagName === "BLOCKQUOTE").length, 1, "quote after formula lost: " + dump(root));
assert.equal(text(find(root, (n) => n.tagName === "BLOCKQUOTE")[0]), "引用", "quote after formula text");
// Tables work inside quotes.
root = render(appendMarkdown, "> | a | b |\n> |---|---|\n> | 1 | 2 |");
assert.equal(find(root, (n) => n.tagName === "BLOCKQUOTE" && find(n, (m) => m.tagName === "TABLE").length).length, 1, "quoted table: " + dump(root));
`, "@BQ@", "`")
	dir := t.TempDir()
	path := filepath.Join(dir, "markdown_rendering.js")
	if err := os.WriteFile(path, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("node", path).CombinedOutput(); err != nil {
		t.Fatalf("bubble markdown rendering: %v\n%s", err, out)
	}
}
