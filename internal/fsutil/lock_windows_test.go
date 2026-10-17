//go:build windows

package fsutil

import (
	"path/filepath"
	"testing"
)

// AppendJSONL locks the file it appends to. An append-only handle lacks the
// read or write-data access LockFileEx needs, so this fails without it.
func TestAppendJSONLLocksOnWindows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.jsonl")
	for i := 0; i < 2; i++ {
		if err := AppendJSONL(path, map[string]int{"n": i}); err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	if err := ReadJSONL(path, func([]byte) error { n++; return nil }); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("lines %d", n)
	}
}
