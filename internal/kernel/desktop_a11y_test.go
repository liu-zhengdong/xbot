package kernel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xbot/internal/desktop"
	"xbot/internal/link"
	"xbot/internal/review"
)

// A driver at the organ boundary, not a separately constructed tool list.
// Real CLI process/argv/delivery tests live in internal/desktop.
type seamDesktop struct{ value string }

func (*seamDesktop) Screenshot(context.Context, string) error   { return nil }
func (*seamDesktop) Click(context.Context, int, int) error      { return nil }
func (*seamDesktop) Type(context.Context, string) error         { return nil }
func (*seamDesktop) Scroll(context.Context, int) error          { return nil }
func (*seamDesktop) Key(context.Context, string) error          { return nil }
func (*seamDesktop) Idle(context.Context) (time.Duration, bool) { return 0, false }
func (d *seamDesktop) Execute(_ context.Context, a desktop.Action) (string, error) {
	if a.Name == "snapshot" {
		return `{"ok":true,"data":{"ref":"@seam:e1","value":"` + d.value + `"}}`, nil
	}
	if a.Name == "type" {
		d.value = a.Text
	}
	return `{"ok":true}`, nil
}

func TestDesktopAccessibilityOrganSeam(t *testing.T) {
	b := testBot(t)
	// No grants. Old stored desktop grant cannot be a prerequisite.
	k := &Kernel{Bot: b}
	found := false
	for _, l := range k.snapshotLinks() {
		if l.Name() == link.NameDesktop {
			l.(*desktop.Link).Driver = &seamDesktop{}
			found = true
		}
	}
	if !found {
		t.Fatal("desktop organ not mounted")
	}
	r := k.toolRegistry()
	for _, name := range []string{"desktop_snapshot", "desktop_set_value", "desktop_click", "desktop_type"} {
		e, ok := r.find(name)
		if !ok || e.source != "organ" || !review.Known(name) || !review.MachineTouching(name) {
			t.Fatal("missing organ/review seam", name)
		}
		for _, mode := range []string{"allow", "ask", "off"} {
			k.Machine = func() (string, error) { return mode, nil }
			need, _, err := k.reviewCall(context.Background(), e.tool, json.RawMessage(`{}`), "")
			if (mode == "off") != (err != nil) || (mode == "ask") != need {
				t.Fatalf("%s %s: %v %v", name, mode, need, err)
			}
		}
	}
	if review.ClassOf("desktop_snapshot") != review.Readonly || review.ClassOf("desktop_set_value") != review.Normal {
		t.Fatal("wrong classification")
	}
	// Actual registry entry → original Link → Perform → driver → observation.
	call := func(name, args string) string {
		t.Helper()
		e, _ := r.find(name)
		out, err := e.tool.Run(context.Background(), json.RawMessage(args))
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	before := call("desktop_snapshot", `{"app":"an app"}`)
	call("desktop_click", `{"ref":"@seam:e1"}`)
	call("desktop_type", `{"ref":"@seam:e1","text":"changed"}`)
	after := call("desktop_snapshot", `{"app":"an app"}`)
	if before == after || !strings.Contains(after, "changed") {
		t.Fatal(after)
	}
	// Production permissive review adds neither a card nor a second off gate.
	k.Permissive = true
	for _, mode := range []string{"ask", "off"} {
		k.Machine = func() (string, error) { return mode, nil }
		e, _ := r.find("desktop_snapshot")
		need, _, err := k.reviewCall(context.Background(), e.tool, nil, "")
		if need || err != nil {
			t.Fatal(need, err)
		}
	}
	if _, err := os.Stat(filepath.Join(b.Root, "apps.json")); err == nil {
		t.Fatal("organ required an app grant")
	}
}
