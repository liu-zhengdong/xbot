package web

import (
	"os/exec"
	"strings"
	"testing"
)

// The starters' profile cache: a bot named like an Object property still gets
// its profile fetched, and a fetch in flight is shared with the info panel.
func TestStarterProfileCache(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	b, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	start := strings.Index(js, "  const starterProfiles =")
	end := strings.Index(js, "\n  function starterList")
	if start < 0 || end < start {
		t.Fatal("starter profile block not found")
	}
	script := `
const state = { panelData: null };
const calls = [];
const api = (path) => { calls.push(path); return new Promise((r) => setTimeout(() => r({ name: path.split("/").pop() }), 5)); };
` + js[start:end] + `
(async () => {
  const p = await starterProfile("constructor");
  if (typeof p !== "object" || p.name !== "constructor") throw new Error("constructor got " + typeof p);
  const a = starterProfile("ada");
  const panel = typeof fetchBotProfile === "function" ? fetchBotProfile("ada") : api("/api/bots/ada");
  await Promise.all([a, panel]);
  const ada = calls.filter((c) => c === "/api/bots/ada").length;
  if (ada !== 1) throw new Error("ada fetched " + ada + " times");
})().catch((e) => { console.error(e.message); process.exit(1); });
`
	out, err := exec.Command(node, "-e", script).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
}
