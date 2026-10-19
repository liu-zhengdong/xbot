//go:build unix

package work

import (
	"os"

	"golang.org/x/sys/unix"
)

// openPastedFile opens a pasted picture without following a symlink at the
// final component. Checking the name and then reading it by path leaves a gap:
// the entry can be swapped for a link in between. Opening this way closes it,
// the same way internal/kernel reads project files.
func openPastedFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
}
