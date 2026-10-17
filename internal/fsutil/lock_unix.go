//go:build unix

package fsutil

import (
	"errors"
	"os"
	"syscall"
)

// appendAccess is the access mode AppendJSONL opens with.
const appendAccess = os.O_WRONLY

// IsWouldBlock reports whether err means the lock is held elsewhere.
func IsWouldBlock(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}

func lockFile(f *os.File, exclusive, wait bool) error {
	how := syscall.LOCK_SH
	if exclusive {
		how = syscall.LOCK_EX
	}
	if !wait {
		how |= syscall.LOCK_NB
	}
	return syscall.Flock(int(f.Fd()), how)
}

func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
