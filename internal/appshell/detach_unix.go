//go:build unix

package appshell

import "syscall"

// detachAttr puts the relaunch in a session of its own: ending this process
// (and the job it may belong to) must not take the new one with it.
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
