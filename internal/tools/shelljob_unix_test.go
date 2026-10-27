//go:build unix

package tools

import (
	"syscall"

	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func processAlive(pid int) bool { return pid > 0 && syscall.Kill(pid, 0) == nil }

// One record that still cannot be rewritten after its output path is pulled
// back, here because the directory is not writable, is skipped: the rest of
// the table still loads.
func TestLoadSkipsUnwritableRecord(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	done := "sh_1abc12de"
	finished, err := json.Marshal(Job{
		ID: done, Command: "true", Status: "done",
		Output: filepath.Join(dir, done+".log"), Started: time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, done+".json"), finished, 0600); err != nil {
		t.Fatal(err)
	}
	bad := "sh_2abc12de"
	running, err := json.Marshal(Job{
		ID: bad, Command: "sleep 30", Status: "running",
		Output:  filepath.Join(t.TempDir(), bad+".log"),
		Started: time.Unix(1, 0).UTC(), PID: 1 << 22, ProcStart: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, bad+".json"), running, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })

	jobs := NewJobs(dir)
	if err := jobs.Load(); err != nil {
		t.Fatal(err)
	}
	var have []string
	for _, job := range jobs.List() {
		have = append(have, job.ID)
	}
	if len(have) != 1 || have[0] != done {
		t.Fatalf("loaded %v, want only %s", have, done)
	}
}
