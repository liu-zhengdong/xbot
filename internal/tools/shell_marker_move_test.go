package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestShellCwdMarkerFollowsRenameThroughOpenFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX descriptor inheritance")
	}
	binary, err := exec.LookPath("sh")
	if err != nil {
		t.Skip(err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldDir, newDir := filepath.Join(root, "before"), filepath.Join(root, "after")
	project := filepath.Join(oldDir, "project")
	if err := os.MkdirAll(project, 0700); err != nil {
		t.Fatal(err)
	}
	markerDir := filepath.Join(oldDir, "cwd-markers")
	if err := os.Mkdir(markerDir, 0700); err != nil {
		t.Fatal(err)
	}
	current := func() string {
		if _, err := os.Stat(oldDir); os.IsNotExist(err) {
			return newDir
		}
		return oldDir
	}
	jobs := NewJobs(filepath.Join(root, "jobs"))
	defer jobs.Close()
	shell := &Shell{interpreter: interpreter{name: "sh", path: binary}, Dir: project, Jobs: jobs}
	var ended string
	shell.BindSession(func(context.Context) (string, string) { return filepath.Join(current(), "project"), "" }, func(_ context.Context, dir string) (string, string) { ended = dir; return dir, "" })
	shell.SetMarkerDir(markerDir)
	shell.BindMarkerIO(func(fn func(string) error) error { return fn(filepath.Join(current(), "cwd-markers")) })
	command := "mv " + shQuote(oldDir) + " " + shQuote(newDir) + "\nprintf '{\"renamed\":true}\\n'"
	raw, _ := json.Marshal(map[string]string{"description": "Rename source while preserving its cwd marker", "command": command})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := shell.Run(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output, "No such file") || !strings.Contains(output, `{"renamed":true}`) {
		t.Fatal(output)
	}
	if ended != filepath.Join(newDir, "project") {
		t.Fatalf("actual cwd %q output %s", ended, output)
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("former directory revived: %v", err)
	}
	for _, script := range []string{`printf '{"next":true}\n'`, `exit 7`} {
		raw, _ = json.Marshal(map[string]string{"description": "Check the original interpreter after rename", "command": script})
		output, err = shell.Run(ctx, raw)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(output, "No such file") || ended != filepath.Join(newDir, "project") {
			t.Fatalf("output %s cwd %s", output, ended)
		}
	}
	entries, err := os.ReadDir(filepath.Join(newDir, "cwd-markers"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("marker cleanup %v %v", entries, err)
	}
}
