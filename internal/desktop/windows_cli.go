package desktop

import (
	"fmt"
	"os"
	"path/filepath"
)

// npm exposes a .cmd shim on Windows. Do not feed user text through cmd.exe:
// resolve the native executable in the official global/local npm layout.
// Names follow agent-desktop 1.0.2 lib/platform.js and lib/windows-bin.js.
func windowsNativeCLI(shim, arch string) (string, error) {
	if arch == "amd64" {
		arch = "x64"
	}
	name := "agent-desktop-win32-" + arch + ".exe"
	dir := filepath.Dir(shim)
	for _, base := range []string{
		filepath.Join(dir, "node_modules", "agent-desktop", "bin"),
		filepath.Join(filepath.Dir(dir), "agent-desktop", "bin"),
	} {
		path := filepath.Join(base, name)
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			return path, nil
		}
	}
	return "", fmt.Errorf("desktop: npm shim cannot be executed directly; set XBOT_AGENT_DESKTOP to the installed native %s", name)
}
