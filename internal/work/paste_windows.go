//go:build windows

package work

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// openPastedFile opens a pasted picture without following a reparse point at
// the final component, in one call, like O_NOFOLLOW on unix. Any reparse point
// is refused, not only symlinks.
func openPastedFile(path string) (*os.File, error) {
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
		return nil, errors.New("work: pasted picture is a reparse point")
	}
	return os.NewFile(uintptr(h), path), nil
}
