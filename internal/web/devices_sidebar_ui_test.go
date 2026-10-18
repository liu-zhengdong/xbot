package web

import (
	"strings"
	"testing"
)

// The sidebar groups other computers' bots by device with a device icon and
// 在 X 上; while the gateway is down there is one notice and the groups fade,
// with 离线 per device only when the gateway is up.
func TestDevicesSidebarUI(t *testing.T) {
	html, _ := staticFS.ReadFile("static/index.html")
	if !strings.Contains(string(html), "<nav id=\"roster\" class=\"list\" aria-label=\"Bot\"></nav>\n      <nav id=\"remote-roster\" class=\"list g7-remote\"") {
		t.Error("remote-roster must follow the roster, outside what app.js re-renders")
	}
	js, _ := staticFS.ReadFile("static/devices.js")
	for _, want := range []string{
		`(up ? "" : ` + "`" + `<div class="g7-side-down"><span>连不上网关，其他电脑的 Bot 暂时用不了。</span>`,
		`const off = !up || !d.online`, `${up && !d.online ? ` + "`" + `<span class="g7-side-off">离线</span>` + "`" + ` : ""}`,
		`<span class="folder-name">${svg("laptop", 13)}<span>${esc(d.label)}</span></span>`,
		`<span class="preview">在 ${esc(d.label)} 上</span>`, `paintSidebar();`,
	} {
		if !strings.Contains(string(js), want) {
			t.Errorf("devices.js missing %s", want)
		}
	}
	css, _ := staticFS.ReadFile("static/devices.css")
	if !strings.Contains(string(css), ".g7-side-dev.is-off .folder-bots, .g7-side-dev.is-off .folder-name { opacity: 0.5; }") {
		t.Error("offline groups must fade")
	}
}
