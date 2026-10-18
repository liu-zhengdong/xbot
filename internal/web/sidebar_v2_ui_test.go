package web

import (
	"regexp"
	"strings"
	"testing"
)

// Device prototype v2, items 7, 13, 14 and 15: the list stays a list at 1024
// (240px), pinned bots are not repeated in their groups and show their names,
// the bottom is only 我 with this computer's name, and 连接应用 and 外观 moved
// into the 我 menu. The UI never says 节点.
func TestSidebarPrototypeV2(t *testing.T) {
	read := func(name string) string {
		t.Helper()
		b, err := staticFS.ReadFile("static/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	html, js, css, dev := read("index.html"), read("app.js"), read("app.css"), read("devices.js")

	// 7: no 节点 in what the page shows.
	for _, s := range regexp.MustCompile(`"[^"\n]*节点[^"\n]*"`).FindAllString(js, -1) {
		t.Errorf("app.js still shows 节点: %s", s)
	}

	// 13: only below 960 does the list fold on its own; 240px up to 1100.
	if !strings.Contains(js, "const compact = window.innerWidth >= 768 && window.innerWidth < 960;") ||
		strings.Contains(js, "window.innerWidth >= 768 && window.innerWidth <= 1100") {
		t.Error("syncSidebar must fold the list only below 960px")
	}
	if !strings.Contains(js, "if (window.innerWidth < 960) compactSidebarChoice = collapsed;") {
		t.Error("收起列表 must remember the choice in the same width band as syncSidebar")
	}
	if !strings.Contains(css, "@media (min-width: 768px) and (max-width: 1100px) { :root { --side: 240px; } }") {
		t.Error("the sidebar is 240px wide between 768 and 1100")
	}

	// 14: pinned bots appear once, with their name under the face.
	if strings.Count(js, "!byName[name].pinned") != 1 || !strings.Contains(js, "if (b && !b.pinned) body.append(botRow(b))") {
		t.Error("folders (and the header-less list) must skip pinned bots")
	}
	if !strings.Contains(js, `const label = el("span", "pin-name");`) || !strings.Contains(js, "btn.append(av, label);") {
		t.Error("a pinned bot shows its name under its face")
	}
	if !strings.Contains(js, `add.append(face, el("span", "pin-name pin-label", "新建"));`) {
		t.Error("the pin row ends with 新建")
	}
	if strings.Contains(js, `sectionLabel("群组", `) {
		t.Error("the 群组 heading lost its own +; 新建 covers groups")
	}

	// 15: only 我 at the bottom; the rest is in its menu.
	foot := html[strings.Index(html, `<div class="side-foot">`):strings.Index(html, `</aside>`)]
	for _, gone := range []string{`id="theme"`, `id="connect"`, "connect-pill", "connect-wrap"} {
		if strings.Contains(html, gone) {
			t.Errorf("the sidebar foot still has %s", gone)
		}
	}
	menu := foot[strings.Index(foot, `id="account-menu"`):strings.Index(foot, `id="connect-pop"`)]
	order := []string{`id="account-devices"`, `id="account-connect"`, `id="account-theme"`, `id="collapse"`, `id="logout"`}
	last := -1
	for _, id := range order {
		i := strings.Index(menu, id)
		if i < 0 || i < last {
			t.Errorf("the 我 menu must list 设备, 连接应用, 外观, 收起列表, 退出 in that order (%s)", id)
		}
		last = i
	}
	if !strings.Contains(foot, `<span id="me-device" class="me-device" hidden></span>`) || !strings.Contains(foot, "me-chev") {
		t.Error("我 shows this computer's name and the up/down mark")
	}
	if !strings.Contains(dev, `me.textContent = self.name || self.label || "";`) {
		t.Error("devices.js fills this computer's name under 我 from /api/devices")
	}
	if !strings.Contains(js, `$("me-name").textContent = state.owner.trim() || "我";`) {
		t.Error("我 shows the owner's name when there is one")
	}
	if !strings.Contains(js, "function closeConnect()") || !strings.Contains(js, "closeConnect();\n    closeNewMenu();") {
		t.Error("closing the popovers closes 连接应用 and 新建 too")
	}
}
