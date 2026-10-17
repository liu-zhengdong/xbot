package kernel

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMain points TMPDIR at a repo-local directory. t.TempDir follows TMPDIR,
// and this package's tests must not land files under /tmp.
func TestMain(m *testing.M) {
	root, err := repoTempRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.Setenv("TMPDIR", root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(root)
	os.Exit(code)
}

func repoTempRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			// One directory per process. Several `go test` processes share this
			// package, and TestMain removes the directory when it returns. A
			// shared directory lets the first process to finish delete the
			// bots the others are still writing, so a fork never lands and a
			// test blocks on its release channel until the 10 minute timeout.
			return filepath.Join(dir, ".test-tmp", "kernel", fmt.Sprintf("p%d", os.Getpid())), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from %s", dir)
		}
		dir = parent
	}
}
