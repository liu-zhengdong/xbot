//go:build windows

package browser

import (
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

// browserProcAttr leaves Chrome independent of the CLI.
func browserProcAttr() *syscall.SysProcAttr {
	return nil
}

func signalChrome(pid int) {
	if pid <= 0 {
		return
	}
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
	}
}

func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	return windows.GetExitCodeProcess(h, &code) == nil && code == 259 // STILL_ACTIVE
}
