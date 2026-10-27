package web

import (
	"os/exec"
	"strings"
	"testing"
)

// The draft is highlighted, not rendered: every Markdown token keeps its
// source characters (`**粗**` still shows the asterisks), only http(s)
// [text](url) links become anchors, code spans swallow nested tokens, and the
// whole draft round-trips unchanged. Runs composerNodes under node with a
// minimal document stub, like TestComposerLinkParsing.
func TestComposerMarkdownHighlighting(t *testing.T) {
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	a := strings.Index(s, "const COMPOSER_MAX = 16000;")
	b := strings.Index(s, "  function composerCaretTo(want) {")
	if a < 0 || b <= a {
		t.Fatal("composer helper block not found in app.js")
	}
	script := strings.ReplaceAll(`
const assert = require("node:assert/strict");
const document = {
  createTextNode: (v) => ({ nodeType: 3, nodeValue: String(v) }),
  createElement: (tag) => ({ nodeType: 1, tagName: String(tag).toUpperCase() }),
};
const linkIcon = (a) => a; // favicon is the bubble's helper; not under test here
`+s[a:b]+`
const text = (nodes) => nodes.map((n) => n.nodeValue == null ? n.textContent : n.nodeValue).join("");
const kinds = (nodes) => nodes.map((n) => n.nodeType === 3 ? "text" : (n.tagName === "A" ? "a" : n.className)).join(",");

// Every token keeps its source characters.
let parts = composerNodes("写 @BQ@code x@BQ@ 和 **粗** 与 *斜*");
assert.equal(text(parts), "写 @BQ@code x@BQ@ 和 **粗** 与 *斜*", "round-trip broke");
assert.equal(kinds(parts), "text,md-code,text,md-bold,text,md-italic", "wrong coloring: " + kinds(parts));
assert.equal(parts[1].textContent, "@BQ@code x@BQ@", "code span must keep its backticks");
assert.equal(parts[3].textContent, "**粗**", "bold must keep its asterisks");

// Heading and list markers only at line start, one to three #.
parts = composerNodes("# 一\n## 二\n### 三\n#### 四");
assert.equal(kinds(parts), "md-heading,text,text,md-heading,text,text,md-heading,text,text,text", "heading markers wrong: " + kinds(parts));

// List markers need the space; -not a list stays plain.
parts = composerNodes("- 项A\n* 项B\n-not a list");
assert.equal(kinds(parts), "md-list,text,text,md-list,text,text,text", "list markers wrong: " + kinds(parts));
assert.ok(!parts.slice(6).some((n) => n.className), "-not must stay plain");

// A list star is not italic; a bare *em* still is.
parts = composerNodes("* item *em*");
assert.equal(kinds(parts), "md-list,text,md-italic", "list star misread: " + kinds(parts));

// [text](url) is an anchor with the same safety as bare URLs, and keeps its
// source words.
parts = composerNodes("看 [文档](https://example.com/x) 即点");
assert.equal(kinds(parts), "text,a,text", "md link wrong: " + kinds(parts));
assert.equal(parts[1].href, "https://example.com/x", "href wrong");
assert.equal(parts[1].target, "_blank", "target wrong");
assert.ok(parts[1].rel.includes("noopener"), "noopener missing");
assert.equal(parts[1].textContent, "[文档](https://example.com/x)", "md link must keep its source text");

// Non-http(s) never becomes an anchor, not even inside [..](..).
assert.ok(composerNodes("[坏](javascript:alert(1))").every((n) => n.tagName !== "A"), "javascript: link leaked");
assert.ok(composerNodes("[坏](data:text/html,x)").every((n) => n.tagName !== "A"), "data: link leaked");

// Code spans swallow URLs and markdown inside them.
parts = composerNodes("看 @BQ@https://x.y/a 和 **粗**@BQ@ 好");
assert.equal(kinds(parts), "text,md-code,text", "code span not first: " + kinds(parts));
assert.ok(!parts.some((n) => n.tagName === "A"), "URL inside code must not link");

// Bare URLs keep the old rules: trailing punctuation stays prose.
parts = composerNodes("看 https://example.com/a(b)，好");
assert.equal(kinds(parts), "text,a,text,text", "bare url wrong: " + kinds(parts));
assert.equal(parts[1].textContent, "https://example.com/a(b", "trailing ) was eaten");

// A mixed multi-line draft round-trips exactly.
const draft = "# 标题\n- @BQ@x@BQ@ 和 [文](https://a.b/c) 与 *斜*\n\n尾 https://e.f/g。";
assert.equal(text(composerNodes(draft)), draft, "multi-line round-trip broke");

// Plain prose and too-deep or spaceless markers stay plain text nodes.
assert.ok(composerNodes("就是一句话").every((n) => n.nodeType === 3), "plain prose got colored");
assert.ok(composerNodes("#### 深标题").every((n) => n.nodeType === 3), "4-deep heading must stay plain");
assert.ok(composerNodes("#no-space").every((n) => n.nodeType === 3), "# without space must stay plain");
`, "@BQ@", "`")
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("composer markdown highlighting: %v\n%s", err, out)
	}
}
