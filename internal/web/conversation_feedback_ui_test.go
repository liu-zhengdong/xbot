package web

import (
	"strings"
	"testing"
)

func TestConversationLoadingAndSendFeedback(t *testing.T) {
	read := func(name string) string {
		data, err := staticFS.ReadFile("static/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	js, css := read("app.js"), read("app.css")
	part := func(start, end string) string {
		a := strings.Index(js, start)
		if a < 0 {
			t.Fatalf("missing %s", start)
		}
		b := strings.Index(js[a:], end)
		if b < 0 {
			t.Fatalf("missing end %s", end)
		}
		return js[a : a+b]
	}
	selectBot := part("async function selectBot(", "async function selectGroup(")
	if !strings.Contains(selectBot, "resetConversation();") || strings.Index(selectBot, "resetConversation();") > strings.Index(selectBot, "await loadThread()") {
		t.Fatal("switch must clear the previous conversation before fetching")
	}
	for _, fragment := range []string{
		"state.messages = [];", "state.panelData = null;", "thread-skeleton",
		"panelBody.replaceChildren", "连不上 XBot", "request !== state.threadLoad",
		"request !== state.panelLoad", "await drain()", "restoreSendFeedback(inner)", "state.pendingSends.push(row)",
		"state.pendingSends = [];", "state.firstRender ||", "state.firstRender = false",
		`sendStatus.setAttribute("role", "status")`, `sendStatus.setAttribute("aria-live", "polite")`,
	} {
		if !strings.Contains(js, fragment) {
			t.Errorf("missing loading/reset/stale-response protection: %s", fragment)
		}
	}
	send := part("async function sendText(", "function closeModal(")
	if strings.Index(send, "optimisticMessage(text)") < 0 || strings.Index(send, "optimisticMessage(text)") > strings.Index(send, "await apiFetch(") || strings.Index(send, "liveBubble()") > strings.Index(send, "await apiFetch(") {
		t.Fatal("user bubble and typing feedback must precede the network request")
	}
	for _, fragment := range []string{"发送中", "没发出去，原文还在输入框里", "sendText(text, pending)", "input.value = text", "await loadThread()", "if (accepted)", "markSent();", "pending.accepted = true", "live?.remove()", "input.focus()", "回复出错了"} {
		if !strings.Contains(send, fragment) {
			t.Errorf("missing send/retry feedback: %s", fragment)
		}
	}
	if strings.Contains(js, "reader.cancel()") {
		t.Fatal("switching bots must not cancel the running turn")
	}
	if strings.Contains(send, "setStatus(e.message") || strings.Contains(js, "setStatus(payload.error") {
		t.Fatal("raw send errors must not appear in the header")
	}
	for _, fragment := range []string{"typing-dots", "host.querySelector(\".typing-dots\")?.remove()"} {
		if !strings.Contains(js, fragment) {
			t.Errorf("missing typing lifecycle: %s", fragment)
		}
	}
	if !strings.Contains(css, "@media (prefers-reduced-motion: reduce) { .typing-dots i { animation: none; } }") || !strings.Contains(css, ".bubble:has(> .typing-dots) { display: block; }") {
		t.Fatal("typing bubble must remain visible and respect reduced motion")
	}
}

// Pressing Enter on the restored text used to leave the old failed row below
// the delivered message, and its 重试 sent the text a second time.
func TestComposerResendDropsFailedRow(t *testing.T) {
	data, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	a := strings.Index(js, "async function sendText(")
	send := js[a : a+strings.Index(js[a:], "optimisticMessage(text)")]
	for _, fragment := range []string{"if (!pending)", `row.querySelector(".send-failed")`, `row.querySelector(".bubble")?.textContent === text`, "row.remove()"} {
		if !strings.Contains(send, fragment) {
			t.Errorf("composer resend must drop the stale failed row before the new one: missing %s", fragment)
		}
	}
}
