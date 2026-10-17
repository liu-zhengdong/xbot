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

func processStartTicks(int) int64 { return 0 }

func sameProcess(int, int64) bool { return false }
