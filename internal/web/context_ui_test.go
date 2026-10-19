package web

import (
	"strings"
	"testing"
)

// TestContextAPIWired keeps the route mounted. A handler without a route is
// dead code the UI would call and get 404 from.
func TestContextAPIWired(t *testing.T) {
	data, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `/api/bots/`) || !strings.Contains(string(data), `/context`) {
		t.Fatal("the panel does not call the context route")
	}
}

// TestContextUsesSVGArrows guards the workspace rule: no bare character arrows
// in the UI, because they break on Windows fallback fonts.
func TestContextUsesSVGArrows(t *testing.T) {
	data, err := staticFS.ReadFile("static/app.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	start := strings.Index(src, "function contextView(data)")
	if start < 0 {
		t.Fatal("contextView not found")
	}
	end := strings.Index(src[start:], "function styleChips(data)")
	if end < 0 {
		t.Fatal("could not bound the context section")
	}
	section := src[start : start+end]
	for _, bad := range []string{"→", "←", "->"} {
		if strings.Contains(section, bad) {
			t.Errorf("context panel uses a character arrow %q", bad)
		}
	}
}

// TestContextSnapshotDroppedOnSwitch keeps a stale snapshot from showing under
// another bot's name: the fetch result is per bot and per wake.
func TestContextSnapshotDroppedOnSwitch(t *testing.T) {
	src := readStatic(t, "static/app.js")
	body := functionBody(t, src, "function resetConversation()")
	if !strings.Contains(body, "state.ctxSnap = null;") {
		t.Error("resetting a conversation does not drop the context snapshot")
	}
	if !strings.Contains(body, "state.ctxMenu = false;") {
		t.Error("resetting a conversation leaves the request menu open")
	}
}

func readStatic(t *testing.T, name string) string {
	t.Helper()
	data, err := staticFS.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// functionBody returns the text of one top-level function, up to the next
// top-level function. Enough to assert on the body without parsing JS.
func functionBody(t *testing.T, src, head string) string {
	t.Helper()
	start := strings.Index(src, head)
	if start < 0 {
		t.Fatalf("not found: %s", head)
	}
	rest := src[start+len(head):]
	if end := strings.Index(rest, "\n  function "); end >= 0 {
		return rest[:end]
	}
	return rest
}
