//go:build linux || darwin

package browser

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The ownership check reads a live process's arguments on every platform that
// has them; on macOS it used to read /proc, which is missing, so Stop never
// recognised its own Chrome. The environment is not part of the command line.
func TestProcessCmdlineShowsArgsNotEnv(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "sleep 30", "xbot-owned-arg")
	cmd.Env = []string{"XBOT_ENV_ONLY=xbot-env-marker"}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	// Linux shows an empty command line while the child is still inside exec,
	// which is the window chromeOwned retries through. Wait it out here too.
	var raw []byte
	var err error
	for tries := 0; tries < 100; tries++ {
		raw, err = processCmdline(cmd.Process.Pid)
		if err != nil || len(raw) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	if !strings.Contains(got, "xbot-owned-arg") || !strings.Contains(got, "sleep 30") {
		t.Fatalf("command line %q", got)
	}
	if strings.Contains(got, "xbot-env-marker") {
		t.Fatalf("environment leaked into command line %q", got)
	}
	if !chromeOwned(cmd.Process.Pid, "xbot-owned-arg") || chromeOwned(cmd.Process.Pid, "xbot-env-marker") {
		t.Fatal("ownership read the wrong fields")
	}
}
