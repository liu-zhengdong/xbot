//go:build linux || darwin

package vault

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

// The daemon's one access check is the connecting process's uid. macOS has no
// SO_PEERCRED, so the daemon used to refuse every client there.
func TestPeerUIDIsConnectingUser(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "p.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Skipf("unix socket unavailable: %v", err)
	}
	defer ln.Close()
	dialed := make(chan net.Conn, 1)
	go func() {
		c, err := net.Dial("unix", socket)
		if err != nil {
			close(dialed)
			return
		}
		dialed <- c
	}()
	c, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if d, ok := <-dialed; ok {
		defer d.Close()
	}
	uid, err := peerUID(c)
	if err != nil || uid != os.Getuid() {
		t.Fatalf("uid %d err %v, want %d", uid, err, os.Getuid())
	}
}
