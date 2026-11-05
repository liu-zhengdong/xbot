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
	// The design standard folds the disabled look of every button flavour into
	// one rule; the assertion follows the current selector but keeps the intent.
	disabled := `:is(.ui-button,.btn,.primary,.secondary):disabled {`
	start := strings.Index(css, disabled)
	if start < 0 {
		t.Fatal("app.css missing the disabled-button rule")
	}
	rule := css[start : start+strings.Index(css[start:], "}")]
	for _, want := range []string{"opacity:var(--ui-disabled)", "cursor:default"} {
		if !strings.Contains(rule, want) {
			t.Errorf("disabled-button rule must keep %s: %s", want, rule)
		}
	}
	if !strings.Contains(css, `.field input::placeholder, .field textarea::placeholder { color: var(--muted); }`) {
		t.Error("app.css missing the muted placeholder rule")
	}
	fieldStart := strings.Index(css, ".field input, .field textarea, .field select, .agent input, .task input {")
	if fieldStart < 0 {
		t.Fatal("field input rule not found")
	}
	fieldRule := css[fieldStart : fieldStart+strings.Index(css[fieldStart:], "}")]
	if !strings.Contains(fieldRule, "color: var(--text);") {
		t.Error("typed text in .field inputs must use --text, not the label's --muted")
	}
	// The disabled rule must come after :hover so hover cannot repaint it black.
	if start < strings.Index(css, ".primary:hover") {
		t.Error("the disabled rule must follow .primary:hover so hover cannot repaint it")
	}
}
