package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsNativeCLI(t *testing.T) {
	for _, layout := range []string{"global", "local"} {
		for _, arch := range []string{"amd64", "arm64"} {
			t.Run(layout+"/"+arch, func(t *testing.T) {
				root := t.TempDir()
				shim := filepath.Join(root, "agent-desktop.cmd")
				dir := filepath.Join(root, "node_modules", "agent-desktop", "bin")
				if layout == "local" {
					shim = filepath.Join(root, "node_modules", ".bin", "agent-desktop.cmd")
				}
				suffix := arch
				if suffix == "amd64" {
					suffix = "x64"
				}
				native := filepath.Join(dir, "agent-desktop-win32-"+suffix+".exe")
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(native, []byte("test executable location"), 0600); err != nil {
					t.Fatal(err)
				}
				got, err := windowsNativeCLI(shim, arch)
				if err != nil || got != native {
					t.Fatal(got, err)
				}
			})
		}
	}
	if _, err := windowsNativeCLI(filepath.Join(t.TempDir(), "agent-desktop.cmd"), "amd64"); err == nil {
		t.Fatal("missing native executable accepted")
	}
}
