//go:build unix

package kernel

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// openProjectFile opens path without following a symlink at the final component.
func openProjectFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
}

// projectStamp identifies path by inode, ctime and mtime so a same-size rewrite
// is noticed even if the old mtime is put back.
func projectStamp(path string) string {
	var st unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil {
		return "absent"
	}
	link := st.Mode&unix.S_IFMT == unix.S_IFLNK
	return fmt.Sprintf("%d.%09d:%d:%t:%d:%d.%09d", st.Mtim.Sec, st.Mtim.Nsec, st.Size, link, st.Ino, st.Ctim.Sec, st.Ctim.Nsec)
}
