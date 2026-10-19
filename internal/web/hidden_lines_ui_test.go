package web

import (
	"strings"
	"testing"
)

// TestHiddenLinesAreDecidedByTheField is the regression for the owner seeing a
// note the kernel wrote as if they had said it. The chat used to recognize the
// internal lines by matching their opening text, so every new kind of note
// needed a new pattern and any missed one showed up as an owner bubble.
func TestHiddenLinesAreDecidedByTheField(t *testing.T) {
	js, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(js)

	start := strings.Index(src, "function skipMessage(m)")
	if start < 0 {
		t.Fatal("skipMessage is gone")
	}
	end := strings.Index(src[start:], "\n  }")
	if end < 0 {
		t.Fatal("skipMessage has no end")
	}
	body := src[start : start+end]

	if !strings.Contains(body, "m.internal") {
		t.Fatalf("skipMessage does not read the internal field:\n%s", body)
	}
	for _, guess := range []string{"startsWith", "<environment_", "<memory>", "[继续]", "主人的补充"} {
		if strings.Contains(body, guess) {
			t.Fatalf("skipMessage still guesses from the text (%q):\n%s", guess, body)
		}
	}
	// The control lines sent to a background are hidden by the same field now,
	// not by a separate pattern.
	if strings.Contains(src, `^\[(主人的补充|排队|打断)\]`) {
		t.Fatal("the chat still matches control lines by their text prefix")
	}
}
