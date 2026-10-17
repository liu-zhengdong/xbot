//go:build windows

package fsutil

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// appendAccess adds read access: an append-only handle has neither read nor
// write-data rights, and LockFileEx needs one of them.
const appendAccess = os.O_RDWR

// IsWouldBlock reports whether err means the lock is held elsewhere.
func IsWouldBlock(err error) bool { return errors.Is(err, windows.ERROR_LOCK_VIOLATION) }

// lockFile takes a whole-file LockFileEx lock, the same call as the old flock.
func lockFile(f *os.File, exclusive, wait bool) error {
	var flags uint32
	if exclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	if !wait {
		flags |= windows.LOCKFILE_FAIL_IMMEDIATELY
	}
	return windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, ^uint32(0), ^uint32(0), new(windows.Overlapped))
}

func unlockFile(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, ^uint32(0), ^uint32(0), new(windows.Overlapped))
}
