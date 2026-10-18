//go:build linux

package browser

import "testing"

// TestBrowserProcAttrNoPdeathsig pins that Chrome outlives the CLI and its thread.
func TestBrowserProcAttrNoPdeathsig(t *testing.T) {
	if attr := browserProcAttr(); attr == nil || attr.Pdeathsig != 0 || !attr.Setpgid {
		t.Fatalf("proc attr %+v", attr)
	}
}

// A command line that reads empty while the process is still inside exec is
// not taken as foreign; Stop used to clear the session and leave it running
// (#225, #226, #27).
func TestChromeOwnedWaitsOutEmptyCmdline(t *testing.T) {
	const profile = "/data/browser/profile"
	reads := 0
	old := readCmdline
	readCmdline = func(int) ([]byte, error) {
		reads++
		if reads <= 3 {
			return nil, nil
		}
		return []byte("/usr/bin/chrome\x00--user-data-dir=" + profile + "\x00"), nil
	}
	t.Cleanup(func() { readCmdline = old })
	if !chromeOwned(1234, profile) {
		t.Fatalf("empty command line read as foreign after %d reads", reads)
	}
}
