//go:build linux

package vault

import (
	"fmt"
	"net"
	"syscall"
)

// peerUID reads SO_PEERCRED once from an already-connected unix socket. The
// kernel reports the uid of the process that opened the connection; no per
// request authentication follows.
func peerUID(c net.Conn) (int, error) {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return 0, fmt.Errorf("vault: not a unix connection")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, err
	}
	var ucred *syscall.Ucred
	var serr error
	if err := raw.Control(func(fd uintptr) {
		ucred, serr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if serr != nil {
		return 0, serr
	}
	if ucred == nil {
		return 0, fmt.Errorf("vault: no peer credentials")
	}
	return int(ucred.Uid), nil
}
