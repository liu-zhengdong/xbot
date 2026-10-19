//go:build !unix

package code

import (
	"os/exec"
	"syscall"
)

func codeProcAttr() *syscall.SysProcAttr { return nil }

func killCode(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
