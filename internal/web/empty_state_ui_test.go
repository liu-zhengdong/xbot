package web

import (
	"regexp"
	"strings"
	"testing"
)

// Window review P2 #7 and #8: the empty chat offers starters that only fill
// the composer, and the card is radius 12 with no extra gap after the sidebar.
func TestEmptyChatStartersAndCard(t *testing.T) {
	read := func(name string) string {
		t.Helper()
		b, err := staticFS.ReadFile("static/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	js, css := read("app.js"), read("app.css")

	if !strings.Contains(js, `const EMPTY_LINE = "有什么事，直接说就行。";`) ||
		strings.Count(js, "EMPTY_LINE, who, true)") != 2 {
		t.Error("an empty bot chat says 有什么事，直接说就行。 and offers starters (history and live paths)")
	}
	for _, rule := range []string{
		".empty .avatar.lg { width: 48px; height: 48px; }",
		".empty h2 { font-size: 18px; font-weight: 640;",
		".empty p { margin: 0; font-size: 14px; line-height: 1.6; color: var(--muted); }",
		".starters { display: flex; flex-direction: column; gap: 8px; width: min(420px, 100%); margin-top: 22px; }",
		"width: 100%; border: 0; border-radius: 12px; padding: 11px 16px;\n  background: var(--composer);",
		".starter .icon { width: 16px; height: 16px;",
		".starter:hover { background: var(--selected); }",
	} {
		if !strings.Contains(css, rule) {
			t.Errorf("app.css is missing %q", rule)
		}
	}
	if strings.Contains(js, "function heat()") {
		t.Error("the placeholder activity heatmap is gone from the empty chat")
	}

	// A starter fills and focuses the composer; nothing on its path sends.
	start := strings.Index(js, "function starterList(who) {")
	if start < 0 {
		t.Fatal("no starterList")
	}
	list := js[start : start+strings.Index(js[start:], "\n  }\n")]
	if !strings.Contains(list, `btn.type = "button"`) || !strings.Contains(list, "fillComposer(text)") {
		t.Error("a starter is a plain button that calls fillComposer")
	}
	fill := js[strings.Index(js, "function fillComposer(text) {"):]
	fill = fill[:strings.Index(fill, "\n  }\n")]
	for _, path := range []string{list, fill} {
		if regexp.MustCompile(`requestSubmit|\.submit\(|sendMessage|send\(|api\(`).MatchString(path) {
			t.Errorf("the starter path must never send: %s", path)
		}
	}
	if !strings.Contains(fill, "input.value = text;") || !strings.Contains(fill, "input.focus();") || !strings.Contains(fill, "syncComposer();") {
		t.Error("fillComposer sets the text, updates the send button and focuses the input")
	}

	// The card: radius 12, flush with the sidebar.
	main := regexp.MustCompile(`(?m)^\.main \{\n([^}]*)\}`).FindStringSubmatch(css)
	if main == nil || !strings.Contains(main[1], "margin-left: 0; border-radius: 12px;") {
		t.Error("the card is radius 12 and starts where the sidebar ends")
	}
	if strings.Contains(css, "background: var(--panel); border-radius: 20px;") {
		t.Error("the info panel follows the card's radius 12")
	}
}
