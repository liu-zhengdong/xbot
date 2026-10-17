package web

import (
	"os"
	"strings"
	"testing"
)

// TestDisabledPrimaryAndFieldTextColors pins two live CSS fixes: a disabled
// primary button reads as disabled, and text typed into .field inputs uses the
// normal text color while only placeholders are muted.
func TestDisabledPrimaryAndFieldTextColors(t *testing.T) {
	data, err := os.ReadFile("static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(data)
	for _, want := range []string{
		`.primary:disabled, .primary:disabled:hover, .primary:disabled:active { background: var(--fill); color: var(--muted); opacity: 1; cursor: default; }`,
		`.field input::placeholder, .field textarea::placeholder { color: var(--muted); }`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("app.css missing %s", want)
		}
	}
	start := strings.Index(css, ".field input, .field textarea, .field select, .agent input, .task input {")
	if start < 0 {
		t.Fatal("field input rule not found")
	}
	rule := css[start : start+strings.Index(css[start:], "}")]
	if !strings.Contains(rule, "color: var(--text);") {
		t.Error("typed text in .field inputs must use --text, not the label's --muted")
	}
	// The disabled rule must come after :hover so hover cannot repaint it black.
	if strings.Index(css, ".primary:disabled,") < strings.Index(css, ".primary:hover") {
		t.Error(".primary:disabled must follow .primary:hover")
	}
}
