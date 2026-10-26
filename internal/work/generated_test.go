package work

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func pngBytes(n int) []byte {
	b := make([]byte, n)
	copy(b, "\x89PNG\r\n\x1a\n")
	return b
}

func TestWriteGeneratedStoresUnderGenerated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ada")
	a, err := WriteGenerated(dir, pngBytes(32))
	if err != nil {
		t.Fatal(err)
	}
	b, err := WriteGenerated(dir, pngBytes(32))
	if err != nil {
		t.Fatal(err)
	}
	if a == b || !strings.HasPrefix(a, "generated/") || !strings.HasSuffix(a, ".png") {
		t.Fatalf("paths %q %q", a, b)
	}
	if !ValidPastedID(filepath.Base(a)) {
		t.Fatalf("name shape %q", a)
	}
	got, err := os.ReadFile(filepath.Join(dir, a))
	if err != nil || len(got) != 32 {
		t.Fatalf("read %v %d", err, len(got))
	}
	if _, err := os.Stat(filepath.Join(dir, PastedDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("generated picture touched paste/")
	}
}

func TestWriteGeneratedRefuses(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteGenerated(dir, []byte("plain text, not a picture")); err == nil {
		t.Fatal("text accepted")
	}
	if _, err := WriteGenerated(dir, pngBytes(GeneratedMaxBytes+1)); err == nil {
		t.Fatal("oversized accepted")
	}
	if _, err := WriteGenerated(dir, nil); err == nil {
		t.Fatal("empty accepted")
	}
}

func TestWriteGeneratedStaysInWorkspace(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, GeneratedDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteGenerated(dir, pngBytes(32)); err == nil {
		t.Fatal("wrote through a symlink out of the workspace")
	}
	if names, _ := os.ReadDir(outside); len(names) != 0 {
		t.Fatalf("file escaped: %v", names)
	}
}

func TestWriteGeneratedFullDirectory(t *testing.T) {
	dir := t.TempDir()
	gen := filepath.Join(dir, GeneratedDir)
	if err := os.MkdirAll(gen, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < GeneratedMaxFiles; i++ {
		if err := os.WriteFile(filepath.Join(gen, fmt.Sprint(i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := WriteGenerated(dir, pngBytes(32)); !errors.Is(err, ErrGeneratedFull) {
		t.Fatalf("full dir err %v", err)
	}
}

func TestWriteGeneratedConcurrentLimit(t *testing.T) {
	dir := t.TempDir()
	gen := filepath.Join(dir, GeneratedDir)
	if err := os.MkdirAll(gen, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < GeneratedMaxFiles-4; i++ {
		if err := os.WriteFile(filepath.Join(gen, fmt.Sprint(i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	paths := make(chan string, 32)
	start := make(chan struct{})
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			p, err := WriteGenerated(dir, pngBytes(64))
			if err == nil {
				paths <- p
			} else if !errors.Is(err, ErrGeneratedFull) {
				t.Error(err)
			}
		}()
	}
	close(start)
	wg.Wait()
	close(paths)
	seen := map[string]bool{}
	for p := range paths {
		if seen[p] {
			t.Fatalf("duplicate name %s", p)
		}
		seen[p] = true
	}
	names, err := os.ReadDir(gen)
	if err != nil || len(names) != GeneratedMaxFiles || len(seen) != 4 {
		t.Fatalf("files=%d saved=%d err=%v", len(names), len(seen), err)
	}
}
