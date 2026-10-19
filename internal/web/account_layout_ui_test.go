package web

import (
	"strings"
	"testing"
)

func TestAccountProfileAndResponsiveLayout(t *testing.T) {
	read := func(name string) string {
		t.Helper()
		b, err := staticFS.ReadFile("static/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	html, js, css := read("index.html"), read("app.js"), read("app.css")
	check := func(source string, wants ...string) {
		t.Helper()
		for _, want := range wants {
			if !strings.Contains(source, want) {
				t.Errorf("missing UI contract: %s", want)
			}
		}
	}
	check(html, `id="account"`, `aria-controls="account-menu"`, `id="panel-scrim"`)
	accountStart := strings.Index(html, `id="account-menu"`)
	connectStart := strings.Index(html, `id="connect-pop"`)
	if accountStart < 0 || connectStart < 0 {
		t.Fatal("account and connections must have separate menus")
	}
	account := html[accountStart:connectStart]
	check(account, `id="collapse"`, `id="logout"`)
	if strings.Contains(html[connectStart:], `id="logout"`) ||
		strings.Contains(html[connectStart:], `id="collapse"`) ||
		strings.Contains(html, "connect-sep") {
		t.Error("account actions and divider must leave connections")
	}
	for _, id := range []string{"logout", "collapse"} {
		if strings.Count(html, `id="`+id+`"`) != 1 {
			t.Errorf("%s must retain one existing handler target", id)
		}
	}
	check(js, `$("logout").prepend(icon("logout"))`,
		`$("collapse").replaceChildren(icon("sidebar")`,
		`if (!ev.target.closest(".account-wrap")) closeAccount();`,
		"function closePops() {\n    closeAccount();",
		`const save = $("modal-ok");`, `save.type = "submit";`,
		`modalForm.onsubmit = async (ev) =>`,
		`modalForm.querySelector(".modal-actions").prepend(actions);`,
		`const exp = el("button", "text-btn", "导出模板");`,
		`const imp = el("button", "text-btn", "导入模板");`,
		`modalForm.classList.remove("profile-modal");`,
		`window.innerWidth >= 768 && window.innerWidth < 960`,
		`compactSidebarChoice !== false`, `compactSidebarChoice = collapsed`,
		`localStorage.getItem("xbot-panel-open")`,
		`localStorage.setItem("xbot-panel-open", String(open))`,
		// The panel is a drawer over the card (#243), so it starts closed
		// everywhere and only an explicit "true" reopens it.
		`preference === "true"`,
		`rememberPanel(open);`, `rememberPanel(false);`,
		`$("panel-scrim").addEventListener("click", () => $("panel-close").click());`,
		`window.innerWidth <= 1100 && panel.classList.contains("open") && !drawer`)
	check(css, ".account-menu button", ".profile-modal #modal-fields { min-height: 0; overflow: auto; }",
		".profile-modal .modal-actions", "position: sticky; bottom: 0; flex: none;",
		// The scrim covers the card the drawer covers (#243), not the viewport.
		".panel-scrim", "position: absolute; inset: 0;",
		"width: min(90%, 400px) !important",
		".in-app .shell", ".appbar {", "--app-region: drag;", "--app-region: no-drag;")
	if strings.Contains(js, `$("modal-cancel").textContent = "关闭"`) {
		t.Error("profile footer must use cancel instead of a separate close row")
	}
}
