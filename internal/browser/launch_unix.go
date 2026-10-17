//go:build unix

package browser

import (
	"os"
	"syscall"
)

// browserProcAttr puts Chrome in its own process group so Stop can signal the
// group. There is no Pdeathsig: that kills Chrome when the CLI exits, and also
// when the Go thread that called Start exits.
func browserProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

func signalChrome(pid int) {
	if pid <= 0 {
		return
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		p, findErr := os.FindProcess(pid)
		if findErr == nil {
			_ = p.Signal(syscall.SIGTERM)
		}
	}
}

func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}
