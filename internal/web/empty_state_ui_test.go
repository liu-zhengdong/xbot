package web

import (
	"regexp"
	"strings"
	"testing"
)

// Empty chat starters fill the composer without sending a message.
func TestEmptyChatStarters(t *testing.T) {
	read := func(name string) string {
		t.Helper()
		b, err := staticFS.ReadFile("static/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	js := read("app.js")

	if !strings.Contains(js, `const EMPTY_LINE = "有什么事，直接说就行。";`) ||
		strings.Count(js, "EMPTY_LINE, who, true)") != 2 {
		t.Error("an empty bot chat says 有什么事，直接说就行。 and offers starters (history and live paths)")
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
}
