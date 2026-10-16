//go:build !linux

package vault

import (
	"fmt"
	"net"
)

// peerUID is unsupported off Linux. The daemon refuses every connection so the
// fallback local Store stays the only usable backend there.
func peerUID(net.Conn) (int, error) {
	return 0, fmt.Errorf("vault: SO_PEERCRED is only available on linux")
}
