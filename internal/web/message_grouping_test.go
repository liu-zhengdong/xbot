package web

import (
	"os/exec"
	"strings"
	"testing"
)

func TestMessageGroupingBoundaries(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	data, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	a := strings.Index(src, "  const GROUP_GAP_MS")
	b := strings.Index(src, "  function cronText(")
	if a < 0 || b < a {
		t.Fatal("message grouping function not found")
	}
	script := src[a:b] + `
const at = ms => ({time: new Date(Date.UTC(2026, 9, 21, 17, 4) + ms).toISOString()});
const checks = [
  ["same instant", at(0), at(0), "bot:a", "bot:a", false],
  ["four minutes", at(0), at(240000), "bot:a", "bot:a", false],
  ["five minutes inclusive", at(0), at(300000), "bot:a", "bot:a", false],
  ["over five minutes", at(0), at(300001), "bot:a", "bot:a", true],
  ["time runs backwards", at(0), at(-1), "bot:a", "bot:a", true],
  ["different sender", at(0), at(1), "bot:a", "bot:b", true],
  ["owner in between", at(0), at(1), "user", "bot:a", true],
  ["system line in between", at(0), at(1), "", "bot:a", true],
  ["first message", null, at(1), "", "bot:a", true],
  ["previous time missing", {}, at(1), "bot:a", "bot:a", true],
  ["current time missing", at(0), {}, "bot:a", "bot:a", true],
  ["bad time", at(0), {time:"invalid"}, "bot:a", "bot:a", true],
  ["remote timestamps", {at:at(0).time}, {at:at(240000).time}, "bot:a", "bot:a", false],
  ["peer timestamps", {ts:at(0).time}, {ts:at(240000).time}, "bot:a", "bot:a", false],
];
const failures = [];
for (const [label, prev, cur, prevKey, key, want] of checks) {
  const got = groupStart(prev, cur, prevKey, key);
  if (got !== want) failures.push(label+": groupStart="+got+", want "+want);
}
if (failures.length) throw new Error(failures.join("\n"));
`
	if output, err := exec.Command(node, "-e", script).CombinedOutput(); err != nil {
		t.Fatalf("message grouping behavior: %v\n%s", err, output)
	}
}
