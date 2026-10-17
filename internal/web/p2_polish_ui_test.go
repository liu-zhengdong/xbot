package web

import (
	"strings"
	"testing"
)

func TestP2UIContracts(t *testing.T) {
	for file, wants := range map[string][]string{
		"static/app.js": {
			`["library", "记忆"], ["computer", "任务"]`, `labeled("人设", persona)`, `"k", "人设"`,
			`还没有记住任何事。对它说「记住……」就会出现在这里`,
			`let label = "自定义时间"`, `parts.length === 5`, `hours.map(Number).join("、")`,
			`" 点每小时"`, `" 分钟"`, `" 小时"`, `"工作日 "`, `"每周"`, `"每月 "`,
			`String(Number(min)).padStart(2, "0")`, `card.title = String(rt.cron || "")`,
			`el("label", "member-option")`, `cb.type = "checkbox"`, `box.querySelectorAll("input:checked")`,
			`icon("check")`, `f.type === "checks" ? "div" : "label"`,
		},
		"static/app.css": {
			`.note .meta { white-space: nowrap; flex: none; }`,
			`position: sticky; top: 0; z-index: 3; flex: none;`, `background: var(--panel); padding: 10px 8px;`,
			`.member-option input:checked ~ svg`, `.member-option:has(input:focus-visible)`, `.member-option:nth-child(odd)`,
		},
	} {
		data, err := staticFS.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !strings.Contains(string(data), want) {
				t.Errorf("%s missing %s", file, want)
			}
		}
		if file == "static/app.js" && strings.Contains(string(data), `"人格"`) {
			t.Error("old persona label remains")
		}
	}
}
