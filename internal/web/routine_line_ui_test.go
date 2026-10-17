package web

import (
	"strings"
	"testing"
)

func TestRoutineWakeIsSystemLine(t *testing.T) {
	js, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	css, err := staticFS.ReadFile("static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`function routineOf(m)`, `parsed.source !== "timer"`,
		`Missed routine times from`, `错过的定时 · `,
		`routine[2].trim()`, `来源：定时`, `icon("clock")`,
		`el("details", "routine-line")`, `el("summary", "sysline")`,
		`el("div", "routine-prompt", routine.prompt)`,
		`m.role === "assistant" && String(m.content || "").trim() === "SILENT"`,
		`lastRoutine.querySelector("summary").append`, `lastRoutine.dataset.branch === (m.branch || "")`, `openRoutines.has(key)`, ` · 没什么要说的`,
		`realPreview(st && st.last_message)`, `if (p && realPreview(p)) return realPreview(p);`,
		`if (!text || text === "SILENT" || summaryLine(text)) continue;`,
	} {
		if !strings.Contains(string(js), want) {
			t.Errorf("missing routine UI contract: %s", want)
		}
	}
	for _, want := range []string{`.routine-line`, `.routine-prompt`, `white-space: pre-wrap`, `font: inherit`} {
		if !strings.Contains(string(css), want) {
			t.Errorf("missing routine styling: %s", want)
		}
	}
}
