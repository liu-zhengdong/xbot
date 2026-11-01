package code

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckoutTaskRelativeLocalRepoUsesProcessDirectory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	root := t.TempDir()
	source := filepath.Join(root, "repo")
	if out, err := exec.Command("git", "init", source).CombinedOutput(); err != nil {
		t.Fatalf("init: %v: %s", err, out)
	}
	t.Chdir(root)
	task := Task{Repo: "./repo", WorkDir: filepath.Join(root, "new-project")}
	got, err := checkoutTask(context.Background(), task, func(fn func(Task) error) error { return fn(task) })
	if err != nil {
		t.Fatalf("relative local clone: %v", err)
	}
	if got.Repo != "./repo" {
		t.Fatalf("caller repo identity changed: %q", got.Repo)
	}
	if _, err := os.Stat(filepath.Join(task.WorkDir, ".git")); err != nil {
		t.Fatal(err)
	}
	origin, err := exec.Command("git", "-C", task.WorkDir, "remote", "get-url", "origin").Output()
	if err != nil {
		t.Fatal(err)
	}
	actual, err := filepath.EvalSymlinks(strings.TrimSpace(string(origin)))
	if err != nil {
		t.Fatal(err)
	}
	expected, err := filepath.EvalSymlinks(source)
	if err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("cloned different source: %q, want %q", actual, expected)
	}
}
