//go:build windows

package tools

import (
	"os/exec"
	"syscall"
)

// cmd.exe does not use CommandLineToArgvW quoting. /s removes the one outer
// quote pair; inner quotes belong to the user's command, not Go's argv codec.
func configureInterpreterCmdLine(cmd *exec.Cmd, script string) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: syscall.EscapeArg(cmd.Path) + ` /d /s /c "` + script + `"`}
}
