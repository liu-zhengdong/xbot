package deviceexec

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"xbot/internal/tools"
)

func TestCapabilitiesReflectInstalledDependencies(t *testing.T) {
	m := testManager(t)
	t.Setenv("XBOT_CHROME", filepath.Join(t.TempDir(), "missing-chrome"))
	t.Setenv("XBOT_AGENT_DESKTOP", filepath.Join(t.TempDir(), "missing-desktop"))
	if runtime.GOOS == "linux" {
		t.Setenv("DISPLAY", "")
	}
	caps := m.Capabilities()
	if !slices.Contains(caps, "read") || !slices.Contains(caps, "write") || slices.Contains(caps, "browser") || slices.Contains(caps, "desktop") {
		t.Fatalf("missing dependency advertised: %v", caps)
	}
	for _, shell := range tools.NewShells() {
		if !slices.Contains(caps, "exec:"+shell.Name()) {
			t.Fatalf("installed interpreter missing: %v", caps)
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XBOT_CHROME", executable)
	if !slices.Contains(m.Capabilities(), "browser") {
		t.Fatal("resolved browser binary not advertised")
	}
}

func TestDefaultExecUsesTargetInterpreter(t *testing.T) {
	m := testManager(t)
	s := testSource()
	if len(tools.NewShells()) == 0 {
		t.Skip("no installed interpreter")
	}
	call := testCall("default", "exec", "", map[string]any{"command": "echo target"})
	submit(t, m, s, call)
	if d := waitDetail(t, m, s, call.Ref); d.Task.Status != "completed" {
		t.Fatalf("default interpreter: %+v", d)
	}
	unavailable := "cmd"
	if runtime.GOOS == "windows" {
		unavailable = "sh"
	}
	call = testCall("unavailable", "exec", unavailable, map[string]any{"command": "echo target"})
	if _, err := m.Submit(t.Context(), s, call); err == nil {
		t.Fatal("missing explicit interpreter silently replaced")
	}
}
