package web

import (
	"strings"
	"testing"
)

// 我 is a line-art portrait from owner_name; the blob with initials is only the
// no-bundle fallback. Dark mode darkens the round container to #a8a8ad.
func TestOwnerAvatarIsPortrait(t *testing.T) {
	read := func(name string) string {
		raw, err := staticFS.ReadFile("static/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	js, css := read("app.js"), read("app.css")
	if !strings.Contains(js, `if (size === "me" && !paint.url && !portraitURL(seed, false)) {`) || !strings.Contains(js, `if (name) slot.replaceChildren(avatar(name, null, "me"));`) {
		t.Error("the 我 avatar is drawn from the owner's name, with the blob only as fallback")
	}
	if !strings.Contains(css, `.avatar.me.blob { background: transparent;`) || strings.Contains(css, `.avatar.me { background: transparent;`) {
		t.Error("only the blob fallback drops the round container")
	}
	if !strings.Contains(css, `html[data-theme="dark"] .avatar { background: #a8a8ad; }`) || strings.Contains(css, "#c9c9ce") {
		t.Error("dark container is #a8a8ad")
	}
}
