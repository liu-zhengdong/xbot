//go:build unix

package code

import (
	"os/exec"
	"syscall"
)

// codeProcAttr puts the coding agent in its own process group, so a cancel
// kills the shell and everything it started. Killing only the shell leaves a
// child such as sleep holding the output pipe open, and the card stays
// running until that child exits on its own.
func codeProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

func killCode(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
