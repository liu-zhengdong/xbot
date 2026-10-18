package web

import (
	"strings"
	"testing"
)

// P1 / 5–6 of the window review: bot replies on the card, an owner bubble,
// 5-minute grouping, the bottom-aligned thread column and the round send button.
func TestMessagesAndComposerContract(t *testing.T) {
	read := func(name string) string {
		data, err := staticFS.ReadFile("static/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	js, css := read("app.js"), read("app.css")
	// The declaration body of a selector that opens a line of its own.
	block := func(start string) string {
		a := strings.Index(css, "\n"+start)
		if a < 0 {
			t.Fatalf("missing CSS block %q", start)
		}
		a++
		b := strings.Index(css[a:], "}")
		if b < 0 {
			t.Fatalf("unterminated CSS block %q", start)
		}
		return css[a : a+b]
	}
	// Vars live in :root and in the dark override; take everything before the
	// shell rules so both declarations are in view.
	vars := css[:strings.Index(css, "/* Shell and layout */")]

	for _, want := range []string{
		// One column for messages and composer (header follows it too).
		`.top-inner, .thread-inner, .composer { width: min(720px, 100% - 64px); }`,
		// Short conversations sit at the bottom without breaking scrolling.
		"justify-content: flex-end",
		"margin-top: auto",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("thread column contract missing: %s", want)
		}
	}
	if !strings.Contains(block(".thread-inner {"), "justify-content: flex-end") {
		t.Error("the thread column itself must be bottom-aligned")
	}

	bubble := block(".bubble {")
	for _, want := range []string{"border-radius: 18px", "padding: 9px 15px", "font-size: 15px"} {
		if !strings.Contains(bubble, want) {
			t.Errorf("owner bubble block missing %s", want)
		}
	}
	for _, want := range []string{
		".msg.user .bubble { background: var(--user-bubble); }",
		".bubble-col { max-width: 72%;",
		// Bot replies: no bubble, 15px at line-height 1.7.
		".msg.bot .body > .bubble, .msg.bot .body > .prose { font-size: 15px; line-height: 1.7; }",
		".msg.bot .body > .bubble { background: transparent; border-radius: 0; padding: 0; max-width: 100%; }",
		// Header line: name 13px/620, time 12px #9a9aa0.
		"font-size: 13px; font-weight: 620;",
		".who > time { font-size: 12px; font-weight: 450; color: #9a9aa0; }",
		// Spacing: 22 between senders, 6 inside a group; the time hides until hover.
		".thread-inner > * { margin-top: 22px; }",
		".thread-inner > .msg.cont { margin-top: 6px; }",
		// A group's first bot reply shows its time in the name line only.
		".msg.bot:not(.cont) > .body:has(> .who) > .meta-row > .meta { display: none; }",
		".msg.bot.cont > .body > .meta-row > .meta { opacity: 0;",
		// The owner bubble hugs its text instead of the action row's width.
		".msg.user .bubble-col { align-items: flex-end; }",
		// Long tokens wrap without the bubble; the hovered row floats above the next message.
		".msg.bot .body > .prose { min-width: 0; max-width: 100%; overflow-wrap: anywhere; word-break: break-word; }",
		"z-index: 3; background: var(--bg); border-radius: 8px; padding: 0 4px;",
		// Time and actions hang below a message, outside the spacing rhythm.
		".msg > .bubble-col > .meta-row, .msg > .body > .meta-row { position: absolute; top: 100%; margin-top: 2px; }",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("message rules missing: %s", want)
		}
	}

	// Light values in :root and dark overrides in the dark block.
	for _, want := range []string{
		"--user-bubble: #f0f0f2;", "--user-bubble: #2a2a2e;",
		"--composer: #f3f3f5;", "--composer: #252528;",
		"--send: #111111;", "--send-ink: #ffffff;", "--send-off: #e2e2e6;", "--send-off-ink: #a6a6ac;",
		"--send: #f2f2f2;", "--send-ink: #111111;",
		"--send-off: rgba(255, 255, 255, 0.08);", "--send-off-ink: #6f6f75;",
	} {
		if !strings.Contains(vars, want) {
			t.Errorf("palette missing: %s", want)
		}
	}

	composer := block(".composer {")
	for _, want := range []string{"background: var(--composer)", "border-radius: 24px", "min-height: 52px", "padding: 0 8px 0 12px"} {
		if !strings.Contains(composer, want) {
			t.Errorf("composer missing %s", want)
		}
	}
	if !strings.Contains(block(".dock {"), "16px") {
		t.Error("composer must sit 16px above the bottom")
	}
	if !strings.Contains(css, ".composer textarea::placeholder { color: var(--faint); }") {
		t.Error("placeholder must use the faint colour")
	}
	// The circle rule now sits under the composer; find its own line.
	sendAt := strings.Index(css, "\n.send {\n")
	if sendAt < 0 {
		t.Fatal("missing the send button circle rule")
	}
	send := css[sendAt : sendAt+strings.Index(css[sendAt:], "}")]
	for _, want := range []string{"width: 36px", "height: 36px", "border-radius: 18px"} {
		if !strings.Contains(send, want) {
			t.Errorf("send button missing %s", want)
		}
	}
	for _, want := range []string{
		".send svg { width: 18px; height: 18px; stroke-width: 2; }",
		".send.secondary,\n.send:disabled { background: var(--send-off); color: var(--send-off-ink); }",
		".send.primary:not(:disabled) { background: var(--send); color: var(--send-ink); }",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("send button state missing: %s", want)
		}
	}

	// The grouping rule is a pure helper used by every renderer.
	for _, want := range []string{
		"const GROUP_GAP_MS = 5 * 60 * 1000;",
		"function groupStart(prev, cur, prevKey, key) {",
		"b - a >= GROUP_GAP_MS",
		"!groupStart(lastMsg, m, lastKey, key)",
		"!groupStart(lastMsg, m, lastKey, \"user\")",
		"!groupStart(lastMsg, m, last, key)",
		"!groupStart(lastMsg, m, last, m.from || \"\")",
		// Live appends follow the same rule.
		"function markLiveGroup(inner, row) {",
		// Live rows group against what is on screen: an event line in between breaks it.
		"const last = inner.lastElementChild;",
		// Group-chat rows carry their sender so a live send can group against them.
		"row.dataset.sender = key;",
		"groupStart({ time: last.dataset.time }, { time: row.dataset.time }, last.dataset.sender, row.dataset.sender)",
		"markLiveGroup(inner, row);",
		// Bot replies render prose directly when they are the Bot's own turn.
		"appendRich(body, displayText(m.content), true)",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("grouping/render contract missing: %s", want)
		}
	}
	if n := strings.Count(js, "markLiveGroup(inner, row);"); n != 2 {
		t.Errorf("a live reply and an optimistic send must both group live, got %d call sites", n)
	}
}
