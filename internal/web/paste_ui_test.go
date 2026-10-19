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
		`const owner = state.sel;`,
		`"/api/bots/" + encodeURIComponent(owner.id) + "/paste"`,
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
	// The batch stays with the conversation it started in, and with the run of
	// that conversation: switching away and back must not let the rest of a
	// batch land in a list that was cleared in between.
	for _, want := range []string{
		`const batch = state.attachBatch;`,
		`if (state.attachBatch !== batch || attachKey(state.sel) !== attachKey(owner)) return;`,
		`state.attachBatch = (state.attachBatch || 0) + 1;`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("an upload batch can outlive its conversation: %s", want)
		}
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
	// A failed send with a picture still restores the words, and says the
	// picture has to be pasted again.
	for _, want := range []string{
		`const pictureNote = images.length ? "；图片要重新贴一次" : "";`,
		`if (!input.value.trim() && text) {`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("a failed send with a picture loses the words: %s", want)
		}
	}
	// Dragging is accepted during dragenter/dragover, when only the item types
	// are readable; reading files there returns nothing.
	for _, want := range []string{
		`const carriesImage = (data) => {`,
		`return items.some((it) => it.kind === "file" && /^image\//i.test(it.type));`,
		`if (!carriesImage(ev.dataTransfer)) return;`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("a dragged file cannot be dropped: %s", want)
		}
	}
	// A refusal with no thumbnail to sit under must still be readable: the bar
	// follows the note, not only the list.
	for _, want := range []string{
		`const has = attachState().length > 0 || !!text;`,
		`$("attach-bar").hidden = !has;`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("a refused picture explains nothing: %s", want)
		}
	}
	// A result for a batch that is already gone must not wipe a refusal the
	// owner is reading in the conversation they are in now.
	for _, want := range []string{
		`removeAttach(row, true); return;`,
		`const stale = state.attachBatch !== batch || attachKey(state.sel) !== attachKey(owner);`,
		`removeAttach(row, stale);`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("a stale upload result can clear the current conversation's note: %s", want)
		}
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
