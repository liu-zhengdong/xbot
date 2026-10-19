package vault

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// peerUID reads LOCAL_PEERCRED once from an already-connected unix socket,
// macOS's counterpart of SO_PEERCRED. The kernel records the credentials of
// the process that connected; no per request authentication follows.
func peerUID(c net.Conn) (int, error) {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return 0, fmt.Errorf("vault: not a unix connection")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *unix.Xucred
	var serr error
	if err := raw.Control(func(fd uintptr) {
		cred, serr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if serr != nil {
		return 0, serr
	}
	if cred == nil {
		return 0, fmt.Errorf("vault: no peer credentials")
	}
	return int(cred.Uid), nil
}
