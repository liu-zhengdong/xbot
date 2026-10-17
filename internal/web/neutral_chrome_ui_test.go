package web

import (
	"strings"
	"testing"
)

func TestNeutralAvatarAndMenuChrome(t *testing.T) {
	for file, checks := range map[string]struct{ want, avoid []string }{
		"app.js": {
			want: []string{
				`const tone = hashName(name) % 4`,
				`["#dddddd", "#d3d3d3", "#c9c9c9", "#bfbfbf"][tone]`,
				`["#333333", "#3d3d3d", "#474747", "#515151"][tone]`,
				`blob.setAttribute("fill", "var(--avatar-fill)")`,
				`text.setAttribute("fill", "var(--avatar-ink)")`,
				`BLOBS[hashName(name) % BLOBS.length]`,
				`img.addEventListener("load", () => av.classList.add("has-photo"))`,
				`const mark = icon("pin", "primary-mark")`, `setTip(mark, "主 Bot")`,
				`line.append(primaryMark())`, `el("span", "primary-label", "主 Bot")`,
				`"secondary", "清除头像"`, `"secondary", "停止"`, `"menu-row secondary"`,
				`title === "删除 Bot" ? "text-btn danger" : "secondary"`,
			},
			avoid: []string{`function hslHex`, `paint.bg`, `paint.ink`, `el("i", "badge")`, `"menu-row danger"`, `"text-btn danger", "清除头像"`},
		},
		"app.css": {
			want: []string{
				`.avatar svg { --avatar-fill: var(--avatar-light); --avatar-ink: var(--avatar-letter-light); }`,
				`html[data-theme="dark"] .avatar svg { --avatar-fill: var(--avatar-dark); --avatar-ink: var(--avatar-letter-dark); }`,
				`.menu-rule { height: 8px; }`,
			},
			avoid: []string{`.connect-sep { height: 1px;`},
		},
		"index.html": {avoid: []string{`<hr`}},
	} {
		data, err := staticFS.ReadFile("static/" + file)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, want := range checks.want {
			if !strings.Contains(text, want) {
				t.Errorf("%s missing %q", file, want)
			}
		}
		for _, avoid := range checks.avoid {
			if strings.Contains(text, avoid) {
				t.Errorf("%s retains %q", file, avoid)
			}
		}
	}
}
