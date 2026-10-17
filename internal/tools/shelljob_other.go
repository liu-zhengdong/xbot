//go:build !unix

package tools

import (
	"os/exec"
	"time"
)

func setGroup(cmd *exec.Cmd) {
	cmd.WaitDelay = 500 * time.Millisecond
}

func killProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
