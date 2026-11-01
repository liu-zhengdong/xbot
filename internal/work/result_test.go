package work

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteResultStoresExclusiveBoundedLocalFiles(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	data := []byte(`{"answer":9007199254740993}`)
	first, err := WriteResult(workspace, data, "json")
	if err != nil {
		t.Fatal(err)
	}
	second, err := WriteResult(workspace, data, "json")
	if err != nil {
		t.Fatal(err)
	}
	if first == second || filepath.Dir(first) != ResultDir {
		t.Fatalf("result paths: %q %q", first, second)
	}
	stored, err := os.ReadFile(filepath.Join(workspace, first))
	if err != nil || !bytes.Equal(stored, data) {
		t.Fatalf("stored: %s %v", stored, err)
	}
	info, err := os.Stat(filepath.Join(workspace, first))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("permissions: %v %v", info, err)
	}
	if _, err := WriteResult(workspace, nil, "txt"); err != nil {
		t.Fatal("empty text is a valid result", err)
	}
}

func TestWriteResultRejectsBeforeCreatingWorkspace(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "absent")
	if _, err := WriteResult(workspace, []byte("bad"), "../outside"); err == nil {
		t.Fatal("accepted external suffix")
	}
	if _, err := WriteResult(workspace, make([]byte, ResultMaxBytes+1), "bin"); err == nil {
		t.Fatal("accepted oversized result")
	}
	if _, err := os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatalf("refusal created workspace: %v", err)
	}
}

func TestWriteResultCannotFollowResultsOutsideWorkspace(t *testing.T) {
	workspace, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(workspace, ResultDir)); err != nil {
		t.Skip("symlinks unavailable", err)
	}
	if _, err := WriteResult(workspace, []byte("private"), "txt"); err == nil {
		t.Fatal("followed results symlink outside workspace")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("outside changed: %v %v", entries, err)
	}
}
