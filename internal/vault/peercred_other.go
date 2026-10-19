//go:build !linux && !darwin

package vault

import (
	"fmt"
	"net"
)

// peerUID is unsupported off Linux and macOS. The daemon refuses every
// connection so the fallback local Store stays the only usable backend there.
func peerUID(net.Conn) (int, error) {
	return 0, fmt.Errorf("vault: peer credentials are only available on linux and darwin")
}
