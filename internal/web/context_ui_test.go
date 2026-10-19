package web

import (
	"strings"
	"testing"
)

// TestContextPanelUI pins the panel's contract: a 上下文 tab, the band classes
// the stylesheet colours by, and the full-width audit view. The JS is not run;
// a literal going missing is what this catches.
func TestContextPanelUI(t *testing.T) {
	for file, wants := range map[string][]string{
		"static/app.js": {
			`["context", "上下文"]`,
			`function contextView(data)`,
			`function openContextSheet(req, snap)`,
			`"/context"`,
			`ctx-band`,
			`ctx-seq`,
			`el("span", "", "在完整视图中打开")`,
			// The panel must not invent a figure the kernel did not send.
			`if (snap === null) {`,
		},
		"static/app.css": {
			`.ctx-band {`,
			`.ctx-sheet-wrap {`,
			`--k-sys: #2f2f33;`,
			`html[data-theme="dark"]`,
			`.k-hist { background: var(--k-hist); }`,
		},
	} {
		data, err := staticFS.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !strings.Contains(string(data), want) {
				t.Errorf("%s missing %q", file, want)
			}
		}
	}
}

// TestContextAPIWired keeps the route mounted. A handler without a route is
// dead code the UI would call and get 404 from.
func TestContextAPIWired(t *testing.T) {
	data, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `/api/bots/`) || !strings.Contains(string(data), `/context`) {
		t.Fatal("the panel does not call the context route")
	}
}

// TestContextUsesSVGArrows guards the workspace rule: no bare character arrows
// in the UI, because they break on Windows fallback fonts.
func TestContextUsesSVGArrows(t *testing.T) {
	data, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	start := strings.Index(src, "function contextView(data)")
	if start < 0 {
		t.Fatal("contextView not found")
	}
	end := strings.Index(src[start:], "function styleChips(data)")
	if end < 0 {
		t.Fatal("could not bound the context section")
	}
	section := src[start : start+end]
	for _, bad := range []string{"→", "←", "->"} {
		if strings.Contains(section, bad) {
			t.Errorf("context panel uses a character arrow %q", bad)
		}
	}
}
