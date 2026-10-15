//go:build linux

package tunnel

import "syscall"

// procAttr ties cloudflared to the serve process. Pdeathsig fires when serve
// is hard-killed, and the process group lets Close reach anything it spawned.
func procAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL, Setpgid: true}
}

func killGroup(pid int) error {
	if pid <= 0 {
		return nil
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		return syscall.Kill(pid, syscall.SIGKILL)
	}
	return nil
}
