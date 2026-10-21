package web

import (
	"strings"
	"testing"
)

// Pin the approval transition independently of foreground Bot status and typing.
func TestTaskAwaitingIgnoresForegroundStatusAndPreservesDraft(t *testing.T) {
	raw, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(raw)
	start := strings.Index(js, "  function taskAwaiting(")
	end := strings.Index(js[start:], "  function approvalTaskButton(")
	awaiting := js[start : start+end]
	if strings.Contains(awaiting, "stStatus") {
		t.Error("task approval must not depend on foreground Bot status")
	}
	for _, want := range []string{
		`go.disabled = !inp.value.trim();`, `delete bgState.steerDraft[r.id];`,
		`{ shell: "后台命令", webhook: "Webhook", watch: "监视" }`, `(task ? "子任务「" + task + "」：" : "子任务：")`, `.replace(/\bsh[0-9a-f]{6,}\b/g, "")`,
		`state.sel.type !== "bot"`,
		`latest.set(card.id, { card, branch: m.branch })`,
		`card.state === "pending" && branch === task.id`,
		`const awaitingIDs = new Set(running.filter((r) => taskAwaiting(r, name)).map((r) => r.id))`,
		`awaitingIDs.size !== bgState.awaitingIDs.size`,
		`Array.from(awaitingIDs).some((id) => !bgState.awaitingIDs.has(id))`,
		`!bgState.force && !awaitingChanged) return`,
		`if (!awaitingChanged && Array.from(pop.querySelectorAll("[data-stop-until]"))`,
		`bgState.steerDraft[inp.dataset.task] = inp.value`,
		`bgState.awaitingIDs = awaitingIDs`,
		`const awaiting = awaitingIDs.has(r.id)`,
		`inp.dataset.task = r.id`,
		`inp.value = bgState.steerDraft[r.id] || ""`,
		`inp.addEventListener("input", () => { bgState.steerDraft[r.id] = inp.value; })`,
		`bgState.steerDraft = {}; bgState.awaitingIDs = new Set()`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("missing approval transition/draft contract: %s", want)
		}
	}
}
