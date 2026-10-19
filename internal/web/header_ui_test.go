package web

import (
	"regexp"
	"strings"
	"testing"
)

// The card header is a 56px row inside the message column, with one panel
// control on the right; the bot's actions live at the top of the info panel.
func TestCardHeaderColumn(t *testing.T) {
	read := func(name string) string {
		t.Helper()
		b, err := staticFS.ReadFile("static/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	html, css, js := read("index.html"), read("app.css"), read("app.js")

	// The header, the thread and the composer share one column rule. 860px is
	// the reading measure now the info panel is a drawer, not a column (#243).
	if !strings.Contains(css, ".top-inner, .thread-inner, .composer { width: min(860px, 100% - 64px); }") {
		t.Error("the header, the thread and the composer must share the min(860px, 100% - 64px) column")
	}
	if regexp.MustCompile(`(?m)^\.top-inner \{[^}]*[^-]width:`).MatchString(css) {
		t.Error("no later .top-inner width may override the shared column")
	}
	if !regexp.MustCompile(`(?m)^\.top \{[^}]*padding: 0;`).MatchString(css) {
		t.Error("the header row must not pad the column off the thread's centre")
	}
	if !strings.Contains(css, "mask-image: linear-gradient(to right, #000 calc(100% - 32px), transparent)") {
		t.Error("a long header name fades over its last 32px only")
	}
	if !regexp.MustCompile(`(?m)^\.top \{\n  height: var\(--top-h\);`).MatchString(css) || !strings.Contains(css, "--top-h: 56px;") {
		t.Error("the header must be 56px high (--top-h: 56px)")
	}
	if !strings.Contains(css, "@media (max-width: 767px)") || !strings.Contains(css, "height: var(--top-h); padding: 0 16px;") {
		t.Error("the narrow layout must keep the header's height and side padding")
	}
	if !regexp.MustCompile(`\.title \.avatar\.head \{[^}]*width: 24px; height: 24px; border-radius: 50%;`).MatchString(css) || !strings.Contains(css, ".msg > .avatar { width: 24px; height: 24px; margin-top: 1px; }") {
		t.Error("the header and message faces are 24px and round")
	}
	if !regexp.MustCompile(`font-size: 15px; font-weight: 620`).MatchString(css) {
		t.Error("the header name must be 15px at weight 620")
	}

	// One panel control, with the 「详情」 tooltip.
	if n := strings.Count(html, `id="info"`); n != 1 {
		t.Errorf("want exactly one #info panel button, found %d", n)
	}
	info := regexp.MustCompile(`<button id="info"[^>]*>`).FindString(html)
	if !strings.Contains(info, `aria-label="详情"`) || !strings.Contains(info, `data-tip="详情"`) {
		t.Errorf("the panel button must carry the 详情 label and tooltip: %s", info)
	}
	if !strings.Contains(info, `class="icon-btn"`) {
		t.Error("the panel button must be a 32x32 icon-btn")
	}
	if strings.Contains(html, `id="more"`) || strings.Contains(js, `$("more")`) {
		t.Error("the header's 更多 (···) menu must be gone")
	}

	// In the browser (no strip) the bell stays in the header, left of the panel.
	if strings.Index(html, `id="bell"`) > strings.Index(html, `id="info"`) {
		t.Error("the bell must sit left of the panel button in the header")
	}

	// The former menu actions open the panel as a compact row at its top.
	actions := regexp.MustCompile(`<div id="panel-actions" class="panel-actions"[^>]*></div>`).FindString(html)
	if actions == "" {
		t.Fatal("index.html needs the #panel-actions row")
	}
	if strings.Index(html, actions) < strings.Index(html, `<div class="panel-bar">`) ||
		strings.Index(html, actions) > strings.Index(html, `id="panel-close"`) {
		t.Error("the actions must sit in the panel bar, left of its close button")
	}
	if !strings.Contains(js, `const btn = el("button", "ghost panel-act"`) || !strings.Contains(js, `setTip(btn, label)`) {
		t.Error("each panel action is a 32px ghost icon button with its label as tooltip")
	}
	for _, want := range []string{`"编辑资料"`, `"复刻"`, `"删除"`} {
		if !strings.Contains(js, want) {
			t.Errorf("the panel action row must keep %s", want)
		}
	}
	sync := js[strings.Index(js, "function syncPanelChrome()"):]
	sync = sync[:strings.Index(sync, "\n  function moveMenu")]
	for _, want := range []string{`$("panel-actions")`, "编辑资料", "复刻", `"删除"`} {
		if !strings.Contains(sync, want) {
			t.Errorf("syncPanelChrome must fill the action row with %s", want)
		}
	}
}

// Selecting the same bot again (status refresh, rename) must not stack the
// name: setHeader rebuilds the title's children every call.
func TestSetHeaderReplacesTitle(t *testing.T) {
	b, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	i := strings.Index(js, "function setHeader(")
	if i < 0 {
		t.Fatal("no setHeader")
	}
	body := js[i : i+strings.Index(js[i:], "\n  }\n")]
	if strings.Contains(body, "titleEl.append(") {
		t.Error("setHeader appends the name; a second call would show it twice")
	}
	if !strings.Contains(body, "titleEl.replaceChildren(") {
		t.Error("setHeader must replace the title's children")
	}
}
