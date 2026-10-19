//go:build linux || darwin

package tools

import (
	"os"
	"os/exec"
	"testing"
)

// A job records its process's start time so a later serve can tell a live job
// from a reused pid. macOS used to read /proc and record zero, so a restart
// adopted nothing and marked live jobs lost.
func TestProcessStartTicksIdentifiesProcess(t *testing.T) {
	self := processStartTicks(os.Getpid())
	if self == 0 || processStartTicks(os.Getpid()) != self {
		t.Fatalf("own start %d is not stable", self)
	}
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	child := processStartTicks(cmd.Process.Pid)
	if child == 0 || child == self {
		t.Fatalf("child start %d, own start %d", child, self)
	}
	if !sameProcess(cmd.Process.Pid, child) || sameProcess(cmd.Process.Pid, child+1) {
		t.Fatal("sameProcess does not match on start time")
	}
	if processStartTicks(-1) != 0 {
		t.Fatal("invalid pid has a start time")
	}
}
