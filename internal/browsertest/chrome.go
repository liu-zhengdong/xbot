// Package browsertest selects a Dock-free browser for tests, not product use.
package browsertest

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// Chrome uses an explicit test override, PATH, then an installed Playwright shell.
// Never fall back to a full Chrome app: even headless Chrome can enter the Dock.
func Chrome(t testing.TB) string {
	t.Helper()
	if name := os.Getenv("XBOT_TEST_CHROME"); name != "" {
		p, err := exec.LookPath(name)
		if err != nil {
			t.Fatalf("XBOT_TEST_CHROME: %v", err)
		}
		t.Logf("browser executable: %s", p)
		return p
	}
	if p, err := exec.LookPath("chrome-headless-shell"); err == nil {
		t.Logf("browser executable: %s", p)
		return p
	}
	cache := os.Getenv("PLAYWRIGHT_BROWSERS_PATH")
	if cache == "" {
		home, _ := os.UserHomeDir()
		switch runtime.GOOS {
		case "darwin":
			cache = filepath.Join(home, "Library", "Caches", "ms-playwright")
		case "windows":
			cache = filepath.Join(os.Getenv("LOCALAPPDATA"), "ms-playwright")
		default:
			base := os.Getenv("XDG_CACHE_HOME")
			if base == "" {
				base = filepath.Join(home, ".cache")
			}
			cache = filepath.Join(base, "ms-playwright")
		}
	}
	for _, binary := range []string{"chrome-headless-shell", "chrome-headless-shell.exe"} {
		paths, _ := filepath.Glob(filepath.Join(cache, "chromium_headless_shell-*", "*", binary))
		for i := len(paths) - 1; i >= 0; i-- {
			if p, err := exec.LookPath(paths[i]); err == nil {
				t.Logf("browser executable: %s", p)
				return p
			}
		}
	}
	t.Skip("chrome-headless-shell unavailable; set XBOT_TEST_CHROME to an executable path")
	return ""
}
