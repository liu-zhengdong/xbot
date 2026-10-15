package fsutil

import (
	"os"
	"path/filepath"
	"syscall"
)

// FileLock is an exclusive flock held until Release. The lock drops if the process dies.
type FileLock struct {
	f *os.File
}

// Lock blocks until path can be locked exclusively.
func Lock(path string) (*FileLock, error) {
	return flock(path, syscall.LOCK_EX)
}

// TryLock takes an exclusive lock without waiting. ok is false when another holder has it.
func TryLock(path string) (*FileLock, bool, error) {
	l, err := flock(path, syscall.LOCK_EX|syscall.LOCK_NB)
	if err == syscall.EWOULDBLOCK || err == syscall.EAGAIN {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return l, true, nil
}

func flock(path string, how int) (*FileLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), how); err != nil {
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
	err := syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	cerr := l.f.Close()
	l.f = nil
	if err != nil {
		return err
	}
	return cerr
}
