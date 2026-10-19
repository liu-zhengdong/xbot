//go:build windows

package work

// nonBlockOpen is zero on Windows: there is no FIFO to wait on, and a named
// pipe is not reachable through a file name in this directory.
const nonBlockOpen = 0
