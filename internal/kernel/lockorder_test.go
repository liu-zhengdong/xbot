package kernel

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestKernelLockCountIsKnown fails when the number of mutexes on Kernel changes,
// so adding a lock is a deliberate act that updates the package comment rather
// than something that slips in with a feature.
func TestKernelLockCountIsKnown(t *testing.T) {
	src, err := os.ReadFile("kernel.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	start := strings.Index(body, "type Kernel struct {")
	if start < 0 {
		t.Fatal("Kernel struct not found")
	}
	end := strings.Index(body[start:], "\n}")
	if end < 0 {
		t.Fatal("Kernel struct end not found")
	}
	structBody := body[start : start+end]
	n := strings.Count(structBody, "sync.Mutex")
	once := strings.Count(structBody, "sync.Once")
	const wantMutex, wantOnce = 9, 6
	if n != wantMutex {
		t.Errorf("Kernel has %s mutexes, want %d; update the package lock-order comment and this count", strconv.Itoa(n), wantMutex)
	}
	if once != wantOnce {
		t.Errorf("Kernel has %s sync.Once, want %d", strconv.Itoa(once), wantOnce)
	}
}
