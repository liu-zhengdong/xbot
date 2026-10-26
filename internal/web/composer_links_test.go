package web

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// TestComposerLinkParsing guards the composer's URL rules: only http(s)
// addresses become links, prose glued to an address (including Chinese) stops
// the link where the address ends, trailing punctuation stays prose, and the
// href is always re-derived by parsing so javascript:/data: can never appear.
// The parser functions are pure, so they run under node without a DOM.
func TestComposerLinkParsing(t *testing.T) {
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	a := strings.Index(s, "const COMPOSER_MAX = 16000;")
	b := strings.Index(s, "  function composerCaretTo(want) {")
	if a < 0 || b < 0 || b <= a {
		t.Fatal("composer helper block not found in app.js")
	}
	script := `
const assert = require("node:assert/strict");
// Minimal stand-ins: the helpers only ever create plain nodes.
const document = {
  createTextNode: (v) => ({ nodeType: 3, nodeValue: String(v) }),
  createElement: (tag) => ({ nodeType: 1, tagName: String(tag).toUpperCase() }),
};
` + s[a:b] + `
const texty = (nodes) => nodes.every((n) => n.nodeType === 3);

// A plain sentence stays one text node.
assert.ok(texty(composerNodes("没有链接的一句话")), "plain text must stay plain");

// The link carries its address, opens externally and shows the same words.
let parts = composerNodes("看 https://example.com/a(b)，好");
assert.equal(parts.length, 4, "link splits the sentence into four parts");
assert.equal(parts[0].nodeValue, "看 ");
assert.equal(parts[1].tagName, "A");
assert.equal(parts[1].textContent, "https://example.com/a(b");
assert.ok(parts[1].href.startsWith("https://"), "href must be http(s)");
assert.equal(parts[1].target, "_blank");
assert.ok(parts[1].rel.includes("noopener"));
assert.equal(parts[2].nodeValue, ")", "trimmed punctuation stays prose");
assert.equal(parts[3].nodeValue, "，好", "the rest of the sentence stays prose");
assert.equal(parts.map((n) => n.nodeValue == null ? n.textContent : n.nodeValue).join(""), "看 https://example.com/a(b)，好", "the draft must round-trip unchanged");

// Prose glued to the address stops the link where the address ends.
parts = composerNodes("看一下https://example.com这个和http://x.y/z。");
assert.equal(parts.filter((n) => n.tagName === "A").length, 2, "two addresses");
assert.equal(parts.find((n) => n.tagName === "A").textContent, "https://example.com");
assert.ok(parts.some((n) => n.nodeValue === "这个和"), "Chinese prose must not be swallowed");

// Only http(s): other schemes never highlight.
assert.ok(texty(composerNodes("ftp://a.b 与 javascript:alert(1) 与 file:///etc")), "non-http(s) stays text");

// The href always comes from parsing, never from the raw draft.
assert.equal(composerSafeHref("javascript:alert(1)"), "");
assert.equal(composerSafeHref("data:text/html,<b>x</b>"), "");
assert.ok(composerSafeHref("https://a.b/c").startsWith("https://"));
assert.ok(composerSafeHref("http://a.b/c").startsWith("http://"));

// Trailing punctuation is trimmed, but never past the address itself.
assert.equal(composerURLStop("https://a.b/c.,"), "https://a.b/c");
assert.ok(texty(composerNodes("https://")), "a bare scheme is not a link");
assert.ok(texty(composerNodes("https://)")), "a scheme with no host is not a link");

// The editor serializer: text, <br> and block edges all round-trip to \n.
const text = (v) => ({ nodeType: 3, nodeValue: v });
const elem = (tag, kids) => ({ nodeType: 1, tagName: tag, childNodes: kids });
const root = elem("DIV", [text("abc"), elem("BR", []), elem("DIV", [text("de")]), text("f")]);
assert.equal(composerText(root), "abc\nde\nf");
assert.equal(composerOffsetOf(root, root.childNodes[2].childNodes[0], 1), 5, "caret offset counts breaks as one");
assert.equal(composerOffsetOf(root, root, 0), 0, "root start is not the end of the draft");
assert.equal(composerOffsetOf(root, root, 2), 4, "root offset counts children, including BR");
assert.equal(composerOffsetOf(root, root.childNodes[2], 1), 6, "element offset counts its preceding children");
assert.equal(composerOffsetOf(root, root, 3), 7, "after a block includes its newline");
`
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("composer link parsing: %v\n%s", err, out)
	}
}

// The click path must open the browser and never submit the composer form.
func TestComposerLinkClickNeverSubmits(t *testing.T) {
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	marker := "input.addEventListener(\"click\", (ev) => {"
	start := strings.Index(s, marker)
	if start < 0 {
		t.Fatal("composer click handler not found")
	}
	body := s[start+len(marker):]
	end := strings.Index(body, "\n  });")
	if end < 0 {
		t.Fatal("composer click handler end not found")
	}
	encoded, _ := json.Marshal(body[:end])
	bodyJS := string(encoded)
	script := `
const assert = require("node:assert/strict");
let opened = null, prevented = false, submitted = 0;
const link = { getAttribute: (k) => (k === "href" ? "https://example.com/a" : null) };
const ev = { target: { closest: (sel) => (sel === "a" ? link : null) }, preventDefault() { prevented = true; } };
const handler = new Function("ev", "input", "composerSafeHref", "window", ` + bodyJS + `);
const win = { open: (href, target, rel) => { opened = [href, target, rel]; } };
handler(ev, { contains: () => true }, (u) => u, win);
assert.ok(prevented, "the click default must be consumed");
assert.deepEqual(opened, ["https://example.com/a", "_blank", "noopener"], "the same _blank + noopener path as bubble links must run");
assert.equal(submitted, 0, "no submit may happen");
// An unsafe href must not open anything.
opened = null; prevented = false;
handler({ target: { closest: () => link }, preventDefault() { prevented = true; } },
  { contains: () => true }, () => "", { open: () => { throw Error("must not open"); } });
assert.ok(prevented && opened === null, "a non-http(s) href must not open");
`
	if out, err := exec.Command("node", "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("composer link click: %v\n%s", err, out)
	}
}
