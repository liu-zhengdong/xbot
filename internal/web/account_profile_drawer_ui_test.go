package web

import (
	"strings"
	"testing"
)

func TestProfileDrawer(t *testing.T) {
	for file, wants := range map[string][]string{
		"static/app.js": {
			`modal.classList.add("profile-drawer");`,
			`modal.classList.remove("profile-drawer");`,
			`modalForm.setAttribute("aria-modal", "true");`,
			`modalForm.setAttribute("aria-labelledby", "modal-title");`,
			`profileReturnFocus = document.activeElement;`,
			`name.dataset.profileFocus = "true";`,
			`modalFields.querySelector("[data-profile-focus]").focus();`,
			`target.focus({ preventScroll: true });`,
			`ev.key !== "Tab" || !modal.classList.contains("profile-drawer")`,
			`section("基本", head, labeled("标识", name), labeled("简介", description))`,
			`section("说话方式",`, `section("免打扰",`, `section("人设",`,
			`if (!modal.hidden) { closeModal(); return; }`,
			`if (ev.target === modal) closeModal();`,
			`authenticated = false;
    closeModal();`,
		},
		"static/app.css": {
			`.modal.profile-drawer { padding: 0; place-items: stretch end; }`,
			`width: 420px; max-width: 100%; height: 100%; max-height: none;`,
			`.profile-modal #modal-fields { min-height: 0; overflow: auto; }`,
			`position: sticky; bottom: 0; flex: none;`,
			`@keyframes profile-slide`, `transform: translateX(100%);`,
			`animation: profile-slide 180ms ease-out;`,
			`@media (max-width: 767px) { .profile-drawer .profile-modal { width: 100%; } }`,
			`@media (prefers-reduced-motion: reduce) { .profile-drawer .profile-modal { animation: none; } }`,
		},
	} {
		data, err := staticFS.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !strings.Contains(string(data), want) {
				t.Errorf("%s missing profile drawer contract: %s", file, want)
			}
		}
	}
}
