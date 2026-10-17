//go:build !linux && !windows

package tunnel

import "syscall"

func procAttr() *syscall.SysProcAttr { return nil }

func killGroup(pid int) error {
	if pid <= 0 {
		return nil
	}
	return syscall.Kill(pid, syscall.SIGKILL)
}
