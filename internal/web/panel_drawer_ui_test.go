package web

import (
	"regexp"
	"strings"
	"testing"
)

// The info panel is a drawer over the card, not a column beside the thread
// (#243). These are the three things that keep it one:
//
//   - it lives inside .main, which is the card with overflow:hidden, so the
//     closed panel is clipped instead of widening the page;
//   - its closed state is a transform/visibility pair, never a width, so it
//     takes no layout box and the thread keeps its column;
//   - it starts closed everywhere, because anything else would hide part of
//     the conversation the first time someone opens the app.
func TestInfoPanelIsDrawer(t *testing.T) {
	read := func(name string) string {
		t.Helper()
		b, err := staticFS.ReadFile("static/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	html, css, js := read("index.html"), read("app.css"), read("app.js")

	// 1: the panel is a child of the card, before </main>.
	mainStart := strings.Index(html, `<main class="main">`)
	mainEnd := strings.Index(html, `</main>`)
	panelAt := strings.Index(html, `id="panel"`)
	if mainStart < 0 || mainEnd < 0 || panelAt < 0 {
		t.Fatal("index.html is missing the card or the panel")
	}
	if panelAt < mainStart || panelAt > mainEnd {
		t.Error("the panel must sit inside .main so the card's overflow clips it")
	}

	// 2: closed is off-canvas + hidden, open is neither; no width animation.
	block := func(sel string) string {
		t.Helper()
		at := strings.Index(css, sel)
		if at < 0 {
			t.Fatalf("missing CSS block %q", sel)
		}
		end := strings.Index(css[at:], "}")
		if end < 0 {
			t.Fatalf("unterminated CSS block %q", sel)
		}
		return css[at : at+end]
	}
	closed, open := block(".panel {"), block(".panel.open {")
	if !strings.Contains(closed, "visibility: hidden") {
		t.Error("the closed panel must be hidden, or its off-canvas box stays reachable")
	}
	if !strings.Contains(closed, "translateX(calc(100% + 16px))") {
		t.Error("the closed panel must sit off the card's right edge")
	}
	if strings.Contains(closed, "flex: 0 0") || strings.Contains(open, "flex:") {
		t.Error("the panel must not take a flex column; that is what squeezed the thread")
	}
	if !strings.Contains(open, "transform: none") || !strings.Contains(open, "visibility: visible") {
		t.Error("the open panel must cancel both the transform and the hidden visibility")
	}
	if !regexp.MustCompile(`(?m)^\.main \{[^}]*overflow: hidden`).MatchString(css) {
		t.Error(".main must clip, or the off-canvas drawer widens the page")
	}

	// 3: closed by default; only an explicit past choice reopens it.
	if !strings.Contains(js, `const wide = preference === "true";`) {
		t.Error("the drawer must start closed and only reopen on an explicit choice")
	}
	if strings.Contains(js, "window.innerWidth >= 1440") {
		t.Error("no width may open the drawer on its own now that it covers the thread")
	}

	// 4: Escape closes it at every width, not only in the narrow band.
	if !strings.Contains(js, `if (panel.classList.contains("open")) {`) {
		t.Error("Escape must close the drawer wherever it covers the thread")
	}
	if strings.Contains(js, `panel.classList.contains("open") && window.innerWidth <= 1100`) {
		t.Error("Escape must not be limited to the narrow width band")
	}

	// 5: the remote conversation is a full panel under this drawer, so opening
	// one closes the drawer instead of being covered by it.
	remote := read("remotechat.js")
	if !strings.Contains(remote, `window.dispatchEvent(new Event("xbot:remote-open"))`) {
		t.Error("opening a remote conversation must announce it")
	}
	if !strings.Contains(js, `window.addEventListener("xbot:remote-open", () => {`) {
		t.Error("app.js must close the drawer when a remote conversation opens")
	}

	// 6: the phone sheet starts at the viewport top, so it owns the top inset.
	mobile := css[strings.Index(css, "@media (max-width: 767px)"):]
	if !strings.Contains(mobile, "padding-top: env(safe-area-inset-top)") {
		t.Error("the full-screen phone sheet must keep the top safe area")
	}
}
