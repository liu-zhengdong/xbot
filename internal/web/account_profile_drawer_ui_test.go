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
			`section("人设卡", pickLine, pickHint)`,
			`window.XBotPersonaCards.pick({`,
			`if (!modal.hidden) { closeModal(); return; }`,
			`if (ev.target === modal) closeModal();`,
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

// The drawer is 基本 → 人设卡 → 保存. Speaking style and quiet hours are gone from the product,
// and the drawer no longer edits the persona text or sends those fields.
func TestProfileDrawerHasOnlyBasicsAndPersonaCard(t *testing.T) {
	data, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	start := strings.Index(src, "  function profileForm(data) {")
	end := strings.Index(src, "  function openProfile(data) {")
	if start < 0 || end < start {
		t.Fatal("profileForm not found")
	}
	form := src[start:end]
	basic := strings.Index(form, `section("基本",`)
	card := strings.Index(form, `section("人设卡",`)
	if basic < 0 || card < basic {
		t.Fatalf("drawer order: 基本 %d, 人设卡 %d", basic, card)
	}
	if n := strings.Count(form, `section("`); n != 2 {
		t.Fatalf("drawer has %d sections, want 基本 and 人设卡", n)
	}
	for _, gone := range []string{"说话方式", "免打扰", "language", "quiet_start", "quiet_end", "persona:", `labeled("人设"`} {
		if strings.Contains(form, gone) {
			t.Errorf("profile drawer still has %q", gone)
		}
	}
	for _, file := range []string{"static/app.js", "static/index.html"} {
		raw, err := staticFS.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, gone := range []string{"说话方式", "免打扰", "quiet_start", "quiet_end"} {
			if strings.Contains(string(raw), gone) {
				t.Errorf("%s still mentions %q", file, gone)
			}
		}
	}
}
