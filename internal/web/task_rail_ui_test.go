package web

import (
	"strings"
	"testing"
)

func TestTaskRailApprovalAndControls(t *testing.T) {
	for file, wants := range map[string][]string{
		"static/app.js": {
			`function taskAwaiting(task, name)`, `task.status === "awaiting"`,
			`card.state === "pending" && branch === task.id`,
			`awaiting ? "pause" : "spin"`,
			`if (!awaiting && bgState.steerOpen[r.id])`,
			`approvalTaskButton(data.name, ag.id)`, `if (!awaiting) row.append(field, go)`,
			`["running", "awaiting", "queued"].includes(ag.status)`,
			`else if (ag.status === "queued") top.append(el("span", "", "已排队"))`,
			`approval.dataset.branch = m.branch || ""`,
			`card.dataset.branch === branch`, `primary.focus({ preventScroll: true })`,
			`confirmTaskStop(stop,`, `Date.now() + 3000`, `再点一次停止`,
			`button.replaceChildren(...original); }, 3000)`,
			`pop.querySelectorAll("[data-stop-until]")`,
			`function paintRailStatus()`, `row.dataset.railState === kind`,
			`" · 等你确认" : " · 在跑"`, `icon("awaiting")`,
			`cut > 0 ? sum.slice(0, cut) : sum`,
			`b.append(icon(iconName), el("span", "", label))`, `face.append(icon("person"))`,
			`avatar(name, null, "me")`,
		},
		"static/app.css": {
			`.sidebar.collapsed #roster .rail-status`, `#pin-row .rail-status`,
			`.rail-status .st-spin { animation: none; }`,
		},
	} {
		raw, err := staticFS.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !strings.Contains(string(raw), want) {
				t.Errorf("%s missing task/rail contract: %s", file, want)
			}
		}
		if file == "static/app.js" && strings.Contains(string(raw), `top.append(el("i", "status-dot run"), el("span", "", "进行中"))`) {
			t.Error("task cards must not repeat their running section heading")
		}
	}
}

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
