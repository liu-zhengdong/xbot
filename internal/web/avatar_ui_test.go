package web

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// A bot avatar is a Notionists portrait drawn in the browser from avatar_seed,
// never a blob or initials and never anything the server stores or serves as
// generated SVG. The glyph is only the fallback.
func TestDefaultAvatarIsGeneratedPortrait(t *testing.T) {
	read := func(name string) string {
		raw, err := staticFS.ReadFile("static/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	js, css, sw, html := read("app.js"), read("app.css"), read("sw.js"), read("index.html")

	bundle := strings.Index(html, `<script src="/vendor/xbot-avatar.min.js"></script>`)
	app := strings.Index(html, `<script src="/app.js"></script>`)
	if bundle < 0 || app < 0 || bundle > app {
		t.Fatal("index.html must load /vendor/xbot-avatar.min.js before /app.js")
	}
	if !strings.Contains(sw, `"/vendor/xbot-avatar.min.js"`) {
		t.Error("sw.js does not precache the avatar bundle")
	}
	for _, want := range []string{"XBotAvatar", "portraitURL", "glyphSVG", "data:image/svg+xml"} {
		if !bytes.Contains([]byte(js), []byte(want)) {
			t.Errorf("app.js missing %q", want)
		}
	}
	// The portrait is only ever an <img> src; the glyph comes from DOMParser.
	if strings.Contains(js, "innerHTML = portraitURL") || strings.Contains(js, "innerHTML = svg") {
		t.Error("the portrait SVG must never be assigned through innerHTML")
	}

	start := strings.Index(js, "  function avatar(name, paint, size) {")
	if start < 0 {
		t.Fatal("app.js has no avatar()")
	}
	body := js[start+1:]
	if end := strings.Index(body, "\n  function "); end >= 0 {
		body = body[:end]
	}
	for _, avoid := range []string{"icon-", "favicon", "logo"} {
		if strings.Contains(body, avoid) {
			t.Errorf("avatar() uses %q; the logo is never an avatar", avoid)
		}
	}
	if !strings.Contains(body, `if (size === "me")`) {
		t.Error("the 我 avatar must keep its own path")
	}

	if !strings.Contains(css, "#e4e4e7") || !strings.Contains(css, "#c9c9ce") {
		t.Error("app.css misses the portrait container backgrounds")
	}
	if strings.Contains(css, "46% 54%") || strings.Contains(css, ".avatar.has-photo") {
		t.Error("app.css keeps the irregular blob radius")
	}

	for _, doc := range []string{"../../docs/atlas/ui.md", "../../docs/atlas/multibot.md"} {
		raw, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(raw, []byte("Notionists")) || !bytes.Contains(raw, []byte("CC0")) {
			t.Errorf("%s does not describe the Notionists portrait and its CC0 art", doc)
		}
	}

	// The drawer's 换一张 applies at once; devices.js asks app.js for a face.
	for _, want := range []string{
		`body: { avatar_seed: seed }`, `const res = await api(avatarURLOf(name), { method: "DELETE" });`, `"换一批"`, `"换一张"`,
		`正在上传 `, `"没换成：" + visibleError`, `window.XBotFaces = {`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js missing the avatar picker contract: %s", want)
		}
	}
	for _, want := range []string{"没换成：图片超过 1 MB。", "没换成：只能用 PNG、JPEG、GIF、WebP。"} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js missing the upload reason %q", want)
		}
	}
	if !strings.Contains(read("devices.js"), "window.XBotFaces ? window.XBotFaces.html(") {
		t.Error("devices.js does not draw remote bots with a generated face")
	}
}

func TestNewBotDialogPicksAvatar(t *testing.T) {
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(raw)
	for _, want := range []string{
		`function openModal(title, fields, onOk, okLabel, after) {`,
		`if (after) after(inputs);`,
		`if (!av.follow) body.avatar_seed = av.seed;`,
		`av.seed = base + "#" + n;`,
		`try { await uploadAvatar(name, av.file); await refreshLists(); await loadPanel(); }`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("new bot dialog: missing %s", want)
		}
	}
}

func TestRemoteFacesUseDeviceSeeds(t *testing.T) {
	read := func(name string) string {
		raw, err := staticFS.ReadFile("static/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	dev, chat := read("devices.js"), read("remotechat.js")
	for _, want := range []string{
		`const a = (d && d.bot_avatars && d.bot_avatars[n]) || {};`,
		`return a.custom ? "glyph" : a.avatar_seed || n;`,
		`window.XBotFaces.html(n, seedOf(n, id), size)`,
		`${faceX(b, d.id)}`,
	} {
		if !strings.Contains(dev, want) {
			t.Errorf("devices.js: missing %s", want)
		}
	}
	if strings.Count(chat, "sideFaces(V.bot, V.dev)") != 3 || strings.Contains(chat, "sideFaces(V.bot)") {
		t.Error("remote chat faces are drawn from the device's seed")
	}
}
