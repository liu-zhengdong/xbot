//go:build unix

package tools

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

func setGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 500 * time.Millisecond
}

// killProcess signals the command's process group. The child is the leader
// because setGroup set Setpgid, so children that did not leave the group die too.
func killProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// sameProcess reports that pid is alive and still the process that started
// at start ticks. A zero start never matches.
func sameProcess(pid int, start int64) bool {
	if pid <= 0 || start <= 0 {
		return false
	}
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	return processStartTicks(pid) == start
}
