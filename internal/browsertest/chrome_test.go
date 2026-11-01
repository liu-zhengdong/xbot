package browsertest

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestChromeSelection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix executable fixtures")
	}
	node, _ := exec.LookPath("node")
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	cache := filepath.Join(root, "cache")
	shell := filepath.Join(bin, "chrome-headless-shell")
	cached := filepath.Join(cache, "chromium_headless_shell-123", "chrome-headless-shell-test", "chrome-headless-shell")
	override := filepath.Join(root, "configured-shell")
	for _, p := range []string{shell, cached, override, filepath.Join(bin, "google-chrome")} {
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("PLAYWRIGHT_BROWSERS_PATH", cache)
	t.Setenv("XBOT_TEST_CHROME", "")
	check := func(want string) {
		t.Helper()
		if got := Chrome(t); got != want {
			t.Fatalf("launch executable = %q, want %q", got, want)
		}
		if node != "" {
			out, err := exec.Command(node, "-e", "process.stdout.write(require('./chrome.cjs')())").Output()
			if err != nil || string(out) != want {
				t.Fatalf("JS executable = %q, %v; want %q", out, err, want)
			}
		}
	}
	check(shell)
	t.Setenv("XBOT_TEST_CHROME", override)
	check(override)
	t.Setenv("XBOT_TEST_CHROME", "")
	if err := os.Remove(shell); err != nil {
		t.Fatal(err)
	}
	check(cached) // Full google-chrome remains on PATH, but must not be selected.
}

// Exercise the selected executable, not merely a --headless flag on Chrome.app.
func TestLaunchHeadlessShell(t *testing.T) {
	chrome := Chrome(t)
	if !strings.HasPrefix(filepath.Base(chrome), "chrome-headless-shell") {
		t.Fatalf("acceptance requires a headless-shell executable, got %s", chrome)
	}
	cmd := exec.Command(chrome, "--no-sandbox", "--disable-gpu", "--user-data-dir="+t.TempDir(), "--dump-dom", "data:text/html,<title>headless-shell-launch</title>")
	if cmd.Path != chrome {
		t.Fatalf("launch path = %s, selected %s", cmd.Path, chrome)
	}
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "<title>headless-shell-launch</title>") {
		t.Fatalf("headless-shell launch: %v\n%s", err, out)
	}
	t.Logf("launched executable: %s", cmd.Path)
}

// This is a test-launcher regression, not an assertion about UI source text.
func TestBrowserLaunchersDoNotSelectSystemChrome(t *testing.T) {
	for _, root := range []string{"../web", "../pipe", "../../e2e/smoke"} {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !(strings.HasSuffix(p, "_test.go") || strings.HasSuffix(p, ".cjs")) {
				return nil
			}
			raw, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			for _, forbidden := range []string{"Google Chrome.app/Contents/MacOS/Google Chrome", "channel:'chrome'", `channel: "chrome"`, `"google-chrome", "chromium"`} {
				if strings.Contains(string(raw), forbidden) {
					t.Errorf("%s selects full Chrome (%s); use browsertest shell selection", p, forbidden)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
