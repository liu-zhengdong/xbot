package web

import (
	"strings"
	"testing"
)

// The update control is one small pill in the header, invisible until a newer
// version has downloaded. It must not become a second notification surface:
// only "ready" and "manual" show it, and a click is what installs.
func TestUpdateButtonContract(t *testing.T) {
	for file, want := range map[string][]string{
		"index.html": {
			`<button id="update" class="update-btn" type="button" hidden>`,
			`<span class="update-label">更新</span>`,
		},
		"app.css": {
			`.update-btn[hidden]{display:none}`,
			`@media (prefers-reduced-motion:reduce){.update-btn.busy svg{animation:none}}`,
			`transition:background-color 120ms ease`,
		},
		"app.js": {
			`api("/api/update/install", { method: "POST" })`,
			`updateState.state === "ready" || updateState.state === "manual"`,
			`if (!document.hidden) refreshUpdate().catch(() => {});`,
		},
	} {
		data, err := staticFS.ReadFile("static/" + file)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range want {
			if !strings.Contains(string(data), w) {
				t.Errorf("%s missing %s", file, w)
			}
		}
	}
}

// Nothing that shows the control may also change the conversation: it is a
// header pill, not a card in the thread.
func TestUpdateButtonIsNotInTheThread(t *testing.T) {
	data, err := staticFS.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	thread := html[strings.Index(html, `<div id="thread"`):]
	thread = thread[:strings.Index(thread, `<div class="dock">`)]
	if strings.Contains(thread, "update-btn") {
		t.Fatal("the update control is rendered inside the conversation")
	}
}
