package web

import (
	"strings"
	"testing"
)

func TestSidebarRailAndMemberSelection(t *testing.T) {
	for file, contract := range map[string]struct{ want, avoid []string }{
		"app.css": {
			want: []string{
				".checks .member-option:has(input:checked) { background: var(--fill); font-weight: 600; }",
				".sidebar.collapsed .pin-row .pin.add { order: -1; }",
			},
			avoid: []string{".sidebar.collapsed .pin-row .pin:not(.add) { display: none; }", ".checks .member-option:nth-child(odd)", ".sidebar.collapsed .group-label .icon-btn"},
		},
		"app.js": {
			want: []string{
				`function setTip(node, text, placement) {`,
				`node.dataset.tipPlacement = placement`,
				`host.dataset.tipPlacement === "right" && host.closest(".sidebar.collapsed")`,
				`r.right + 8`, `r.top + (r.height - th) / 2`,
				`setTip(av, name, "right")`, `setTip(btn, name, "right")`,
				`setTip(add, "新建 Bot 或群组", "right")`, `item("plus", "新建 Bot", openNewBot)`,
				`item("users-plus", "新建群组", openNewGroup)`,
				`if (size === "xs") return ""`, `Script=Han`, `if (size === "me" && !paint.url && !portraitURL(seed, false)) {`,
				`chars[0].toUpperCase() + chars[1].toLowerCase()`,
				`Array.from(letter).length > 1 ? "26" : "30"`,
				`svg.style.setProperty("--avatar-light", "#d3d3d3")`,
				`svg.style.setProperty("--avatar-dark", "#3d3d3d")`,
				`svg.style.setProperty("--avatar-letter-light", "#333333")`,
				`svg.style.setProperty("--avatar-letter-dark", "#e4e4e4")`,
				`$("account-theme").addEventListener("click", () => {`,
				`accountTheme.replaceChildren(icon(dark ? "sun" : "moon"), document.createTextNode("外观"), el("span", "menu-count", dark ? "深色" : "浅色"))`,
				`const hasFolders = folders.some((folder) => !folder.ungrouped)`,
				`if (!hasFolders && folder.ungrouped)`, `if (b && !b.pinned) body.append(botRow(b))`, `const body = el("div", "folder-bots")`,
			},
			avoid: []string{`const tone = hashName(name) % 4`, `$("theme")`, `const newer = el("button", "icon-btn")`},
		},
		"index.html": {want: []string{`id="account-theme" type="button">外观</button>`}},
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
