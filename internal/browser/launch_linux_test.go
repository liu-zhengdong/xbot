//go:build linux

package browser

import "testing"

// TestBrowserProcAttrNoPdeathsig pins that Chrome outlives the CLI and its thread.
func TestBrowserProcAttrNoPdeathsig(t *testing.T) {
	if attr := browserProcAttr(); attr == nil || attr.Pdeathsig != 0 || !attr.Setpgid {
		t.Fatalf("proc attr %+v", attr)
	}
}
