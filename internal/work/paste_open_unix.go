//go:build unix

package work

import "syscall"

// nonBlockOpen keeps a read from waiting on a FIFO. A pasted picture is always
// a regular file, but a name can be swapped for something else, and opening a
// FIFO blocks until a writer appears.
const nonBlockOpen = syscall.O_NONBLOCK
