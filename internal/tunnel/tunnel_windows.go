//go:build windows

package tunnel

import (
	"os"
	"syscall"
)

func procAttr() *syscall.SysProcAttr { return nil }

func killGroup(pid int) error {
	if pid <= 0 {
		return nil
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
