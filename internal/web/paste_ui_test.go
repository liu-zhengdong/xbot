package web

import (
	"strings"
	"testing"
)

// TestComposerAcceptsPastedPictures is the guard for the composer half of the
// bug: the textarea had no paste handler, the chat had no drop handler, and the
// send button was gated on text alone. Each assertion names the behavior the
// owner asked for, so a later cleanup cannot quietly drop it.
func TestComposerAcceptsPastedPictures(t *testing.T) {
	read := func(name string) string {
		data, err := staticFS.ReadFile("static/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	js, html, css := read("app.js"), read("index.html"), read("app.css")

	// The pictures waiting to be sent need somewhere to sit in the composer.
	for _, want := range []string{`id="attach-bar"`, `id="attach-list"`, `id="attach-note"`} {
		if !strings.Contains(html, want) {
			t.Errorf("composer has no place for pending pictures: %s", want)
		}
	}
	// Paste and drop both reach the uploader.
	for _, want := range []string{
		`input.addEventListener("paste", (ev) => {`,
		`const files = imagesFrom(ev.clipboardData);`,
		`dropZone.addEventListener("drop", (ev) => {`,
		`uploadAttach(files);`,
		`/api/bots/" + encodeURIComponent(selection.id) + "/paste"`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("paste or drop is not wired to an upload: %s", want)
		}
	}
	// A pasted screenshot must not also land in the textarea as garbage.
	if !strings.Contains(js, `if (!files.length) return;`+"\n"+`    ev.preventDefault();`) {
		t.Error("a pasted picture must prevent the default text paste")
	}
	// The server refuses more than four, so the composer says so first.
	if !strings.Contains(js, `const ATTACH_MAX = 4;`) || !strings.Contains(js, `一次最多 " + ATTACH_MAX + " 张图。`) {
		t.Error("the composer must explain the four-picture limit before uploading")
	}
	// Sending carries the ids and works with no text at all.
	for _, want := range []string{
		`const images = attachReady().map((row) => row.id);`,
		`if (images.length) payload.images = images;`,
		`if ((!text && !images.length) || !state.sel) return;`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("send does not carry pictures or still demands text: %s", want)
		}
	}
	// The transcript paints the picture from the server, so a reload keeps it.
	if !strings.Contains(js, `const pasted = pastedURLs(state.sel, m.images);`) {
		t.Error("a reloaded transcript does not render pasted pictures")
	}
	// Switching bot drops pictures uploaded to the old one.
	if !strings.Contains(js, `clearAttach();`+"\n"+`    state.streaming = 0;`) {
		t.Error("switching conversations must clear pending pictures")
	}
	// One quiet outline while dragging; no animation, so nothing to gate.
	if !strings.Contains(css, `.composer.dropping { box-shadow: inset 0 0 0 2px var(--accent); }`) {
		t.Error("dropping a picture gives no visible target")
	}
	if !strings.Contains(css, `.composer.has-attach {`) {
		t.Error("pending pictures have no layout in the composer")
	}
}
