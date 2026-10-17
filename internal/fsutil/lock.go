package fsutil

import (
	"os"
	"path/filepath"
)

// FileLock is an exclusive lock held until Release. The lock drops if the process dies.
type FileLock struct {
	f *os.File
}

// Lock blocks until path can be locked exclusively.
func Lock(path string) (*FileLock, error) {
	return flock(path, true, true)
}

// TryLock takes an exclusive lock without waiting. ok is false when another holder has it.
func TryLock(path string) (*FileLock, bool, error) {
	l, err := flock(path, true, false)
	if IsWouldBlock(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return l, true, nil
}

// LockFile locks an already open file. exclusive picks a write or read lock;
// wait blocks until the lock is free. A non-waiting call that finds the lock
// held returns an error where IsWouldBlock is true.
func LockFile(f *os.File, exclusive, wait bool) error {
	return lockFile(f, exclusive, wait)
}

// UnlockFile releases a lock taken by LockFile.
func UnlockFile(f *os.File) error {
	return unlockFile(f)
}

func flock(path string, exclusive, wait bool) (*FileLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f, exclusive, wait); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &FileLock{f: f}, nil
}

// Release unlocks and closes the file.
func (l *FileLock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := unlockFile(l.f)
	cerr := l.f.Close()
	l.f = nil
	if err != nil {
		return err
	}
	return cerr
}
