package eval

import "testing"

// skipWithoutJail skips a test that runs cases in the sandbox when this machine
// cannot build one. The jail needs Linux namespaces (unshare) and chroot, so
// these tests run in Linux CI and are skipped on macOS and Windows rather than
// reported as failures there.
func skipWithoutJail(t *testing.T) {
	t.Helper()
	if err := requireJail(); err != nil {
		t.Skipf("sandbox unavailable here: %v", err)
	}
}
