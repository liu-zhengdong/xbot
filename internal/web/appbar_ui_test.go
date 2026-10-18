package web

import (
	"strings"
	"testing"
)

// The desktop app's title bar is a 40px strip: the card starts below it, the
// native window buttons keep their area empty, and the strip drags the window.
func TestDesktopAppBar(t *testing.T) {
	for file, contract := range map[string]struct{ want, avoid []string }{
		"index.html": {
			want: []string{
				`<div id="appbar" class="appbar" hidden>`,
				`<span id="appbar-ver" class="appbar-ver" hidden>`,
				`<div id="appbar-slot" class="appbar-slot"></div>`,
				// The browser UI keeps its bell in the header; only the app moves it.
				`<button id="bell" class="icon-btn" type="button" aria-label="通知" data-tip="通知">`,
			},
		},
		"app.css": {
			want: []string{
				`--strip-h: var(--mygo-titlebar-height, 40px);`,
				`--strip-reserve: var(--mygo-titlebar-inset-right, 0px);`,
				`background: var(--base); --app-region: drag;`,
				`padding: 0 var(--strip-reserve) 0 0;`,
				`.in-app .shell { padding: var(--strip-h) 8px 8px 0; }`,
				`.in-app .main { border-radius: 12px; }`,
				`.in-app .notes { position: fixed; z-index: 40; }`,
				`.in-app.os-mac .appbar-brand { margin-left: calc(72px + 16px); }`,
			},
			avoid: []string{
				// The panel sits in the card below the strip; padding it would
				// stop the closed panel from collapsing.
				`.in-app .panel {`,
				// The strip owns the title bar now; the pieces it passes stay plain.
				`.in-app .top {`,
				`.in-app .panel-bar {`,
				`.in-app .sidebar .pin-row {`,
			},
		},
		"app.js": {
			want: []string{
				`root.classList.add(platform === "darwin" ? "os-mac" : platform === "linux" ? "os-linux" : "os-win");`,
				`document.getElementById("appbar-slot").appendChild(document.getElementById("bell"));`,
				`api("/api/app/theme", { method: "POST", body: { theme: dark ? "dark" : "light" } })`,
				`state.version = (data && data.version) || "";`,
				`if (!notesEl.hidden && inApp) fitNotes();`,
				`window.addEventListener("resize", () => { if (inApp && !notesEl.hidden) fitNotes(); });`,
			},
		},
	} {
		data, err := staticFS.ReadFile("static/" + file)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range contract.want {
			if !strings.Contains(string(data), want) {
				t.Errorf("%s missing %s", file, want)
			}
		}
		for _, avoid := range contract.avoid {
			if strings.Contains(string(data), avoid) {
				t.Errorf("%s retains %s", file, avoid)
			}
		}
	}
}
