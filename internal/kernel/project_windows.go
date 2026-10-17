//go:build windows

package kernel

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// openProjectFile opens path without following a symlink or other reparse
// point at the final component, in one call, like O_NOFOLLOW on unix. Any
// reparse point is refused, not only symlinks.
func openProjectFile(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(name, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		_ = windows.CloseHandle(h)
		return nil, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = windows.CloseHandle(h)
		return nil, errors.New("project file is a reparse point (symlink or similar)")
	}
	return os.NewFile(uintptr(h), path), nil
}

// projectStamp identifies path by size and mtime, with a symlink bit.
// Windows has no inode or ctime here, so a same-size rewrite that restores
// the old mtime is not noticed.
func projectStamp(path string) string {
	st, err := os.Lstat(path)
	if err != nil {
		return "absent"
	}
	link := st.Mode()&os.ModeSymlink != 0
	return fmt.Sprintf("%d:%d:%t", st.ModTime().UnixNano(), st.Size(), link)
}
