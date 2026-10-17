//go:build windows

package kernel

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenProjectFileRefusesSymlinkOnWindows(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.md")
	if err := os.WriteFile(target, []byte("hi"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := openProjectFile(target)
	if err != nil {
		t.Fatalf("regular file: %v", err)
	}
	_ = f.Close()
	link := filepath.Join(dir, "AGENTS.md")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink needs developer mode or admin: %v", err)
	}
	if f, err := openProjectFile(link); err == nil {
		_ = f.Close()
		t.Fatal("opened a symlink")
	}
}
