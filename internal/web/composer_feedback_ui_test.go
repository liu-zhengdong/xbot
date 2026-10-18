package web

import (
	"strings"
	"testing"
)

func TestComposerFeedbackAndTriggerTips(t *testing.T) {
	read := func(name string) string {
		data, err := staticFS.ReadFile("static/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	js, html, css := read("app.js"), read("index.html"), read("app.css")
	for _, want := range []string{
		`.gate-card:has(#login-error:not([hidden])) #login-hint { display: none; }`,
		".send.secondary,\n.send:disabled { background: var(--send-off); color: var(--send-off-ink); }",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("missing conditional guidance or disabled styling: %s", want)
		}
	}
	for _, absent := range []string{`id="plus"`, `id="plus-menu"`, `id="mic"`} {
		if strings.Contains(html, absent) {
			t.Errorf("unbuilt composer control still rendered: %s", absent)
		}
	}
	if !strings.Contains(html, `id="send" class="send secondary" disabled`) || strings.Contains(css, `.composer:not(.has-text) .send { display: none; }`) {
		t.Error("empty composer must show a disabled secondary send button")
	}
	for _, want := range []string{
		`$("send").disabled = !hasText;`,
		`$("send").classList.toggle("primary", hasText);`,
		`$("send").classList.toggle("secondary", !hasText);`,
		`if (!text.trim() || !state.sel) return;`,
		`host.getAttribute("aria-expanded") === "true"`,
		`document.addEventListener("click", hideTip, true);`,
		`$("bell").setAttribute("aria-expanded", String(!notesEl.hidden));`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("missing composer/tooltip state guard: %s", want)
		}
	}
	send := js[strings.Index(js, "  async function sendText("):strings.Index(js, "  function closeModal(")]
	if strings.Contains(send, `"重试"`) || strings.Contains(send, `input.value = ""`) {
		t.Error("failed sends must only resend through the restored composer")
	}
	for _, want := range []string{
		`marker.className = "send-state send-failed";`,
		`marker.replaceChildren(el("span", "", accepted ? "没送到，原文还在输入框里" : "没发出去，原文还在输入框里"));`,
		`input.value = text;`,
		`row.dataset.sendText === text`,
		`"放回输入框"`,
		`row.remove();`,
	} {
		if !strings.Contains(send, want) {
			t.Errorf("missing failed-send preservation/cleanup: %s", want)
		}
	}
	if !strings.Contains(js, `const retry = el("button", "secondary", "重试");`) || !strings.Contains(js, `"连不上 XBot"`) {
		t.Error("thread loading must retain its separate offline retry")
	}
}
