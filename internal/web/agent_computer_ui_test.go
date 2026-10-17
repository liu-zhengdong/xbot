package web

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestClipRunes(t *testing.T) {
	for _, tc := range []struct {
		text string
		n    int
		want string
	}{
		{"  中文任务标题  ", 4, "中文任务…"},
		{"中文", 2, "中文"},
		{"a😀中𠮷文", 4, "a😀中𠮷…"},
		{" abc ", 2, "ab…"},
		{"中文", 0, "…"},
		{"  ", 3, ""},
	} {
		got := clip(tc.text, tc.n)
		if got != tc.want || !utf8.ValidString(got) {
			t.Errorf("clip(%q, %d) = %q; want %q", tc.text, tc.n, got, tc.want)
		}
	}
}

func TestComputerTabLayout(t *testing.T) {
	js, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	css, err := staticFS.ReadFile("static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	source := string(js)
	start := strings.Index(source, "  function computerView(data)")
	end := strings.Index(source[start:], "  async function loadModelCatalog")
	view := source[start : start+end]
	for _, want := range []string{
		`wrap.append(routines, tasks)`, `section("例程")`, `section("进行中")`,
		`ag.status === "running"`, `["done", "failed", "cancelled", "canceled", "interrupted", "denied", "declined"].includes(ag.status)`,
		`top.append(el("i", "status-dot run"), el("span", "", "进行中"))`,
		`"secondary", "引导"`, `"secondary", "停止"`,
		`"没有进行中的任务"`, `"没有例程"`, `"最近结束 · " + finished.length`,
		`state.recentTasksOpen.has(botName)`, `toggle.setAttribute("aria-expanded", String(open))`,
		`list.hidden = !open`, `clip(plainText(displayText(ag.task || ag.id)) || "子任务", 80)`, `taskStatus(ag.status)`,
	} {
		if !strings.Contains(view, want) {
			t.Errorf("computer tab missing %s", want)
		}
	}
	runningStart := strings.Index(view, "for (const ag of running)")
	finishedStart := strings.Index(view, "for (const ag of finished)")
	if runningStart < 0 || finishedStart <= runningStart {
		t.Fatal("task groups missing or out of order")
	}
	if strings.Contains(view[finishedStart:], "controlAgent") || strings.Contains(view, "danger") {
		t.Error("finished tasks must be read-only and stop must be tonal")
	}
	if !strings.Contains(source, "const chars = Array.from(s)") || !strings.Contains(source, `chars.slice(0, n).join("")`) {
		t.Error("JS clipping must preserve characters")
	}
	if !strings.Contains(source, "recentTasksOpen: new Set()") || strings.Contains(view, "localStorage") {
		t.Error("fold state must be memory-only")
	}
	for _, want := range []string{`.recent-task:nth-of-type(even)`, `.recent-status.failed { color: var(--err); }`} {
		if !strings.Contains(string(css), want) {
			t.Errorf("missing task style %s", want)
		}
	}
}
