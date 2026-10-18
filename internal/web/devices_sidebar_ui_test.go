package web

import (
	"strings"
	"testing"
)

// The sidebar groups other computers' bots by device (v2 侧栏 6–9, 16): the
// header is a device icon, the name and 「● 在线」 or 「○ 时间」 (no bot count),
// a device without a bot list says 「等它的 Bot 列表」, and each row is a button
// that opens the remote chat with the last line, 排队中 and the unread dot.
func TestDevicesSidebarUI(t *testing.T) {
	js, _ := staticFS.ReadFile("static/devices.js")
	for _, want := range []string{
		`const nav = document.getElementById("remote-roster");`,
		`<span class="g7-side-name">${svg("laptop", 13)}<span>${esc(d.label)}</span></span>${status(d)}`,
		`<span class="g7-side-st"><i class="status-dot ok"></i>在线</span>`,
		`title="离线 · 最后联系 ${esc(seen)}"`, `等它的 Bot 列表`,
		`data-side="open" data-id="${esc(d.id)}" data-bot="${esc(b)}"`,
		`◷ 排队中</b> · 你：${esc(c.queued)}`, `esc(me + body)`, `<i class="unread"></i>`,
		`连不上网关，其他电脑的 Bot 暂时用不了。`, `if (back) back.focus();`,
		`if (side === "open") { if (D.chat) D.chat.open(t.dataset.id, t.dataset.bot); return; }`,
	} {
		if !strings.Contains(string(js), want) {
			t.Errorf("devices.js missing %s", want)
		}
	}
	for _, avoid := range []string{"在 ${esc(d.label)} 上", "folder-count", "节点"} {
		if strings.Contains(string(js), avoid) {
			t.Errorf("devices.js retains %s", avoid)
		}
	}
	css, _ := staticFS.ReadFile("static/devices.css")
	if strings.Contains(string(css), ".g7-remote-bot { cursor: default; }") || !strings.Contains(string(css), ".g7-side-dev.is-off .g7-remote-bot") {
		t.Error("remote rows must be clickable and fade when offline")
	}
}
