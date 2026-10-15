//go:build !unix

package tools

import (
	"os/exec"
	"time"
)

func configureCmd(cmd *exec.Cmd) {
	cmd.WaitDelay = 500 * time.Millisecond
}
