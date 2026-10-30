package config

import (
	"os"
	"sync"
	"testing"
)

func TestWebImageGenSettings(t *testing.T) {
	root := t.TempDir()
	f, err := LoadWeb(root)
	if err != nil || f.ImageGenOn() {
		t.Fatalf("default: %+v %v", f, err)
	}
	for _, id := range []string{"openai", "xai"} {
		for _, enabled := range []bool{true, false} {
			if err := SetWebImageGen(root, enabled, id, "local"); err != nil {
				t.Fatal(err)
			}
			f, err = LoadWeb(root)
			if err != nil || f.ImageGenOn() != enabled || f.ImageGenProvider != id || f.ImageGenPolicy() != "local" {
				t.Fatalf("roundtrip: %+v %v", f, err)
			}
			before, _ := os.Stat(WebPath(root))
			if err := SetWebImageGen(root, enabled, id, "local"); err != nil {
				t.Fatal(err)
			}
			after, _ := os.Stat(WebPath(root))
			if !os.SameFile(before, after) {
				t.Fatal("identical PUT rewrote file")
			}
		}
	}
	if err := SetWebImageGen(root, true, "unknown", "auto"); err == nil {
		t.Fatal("unknown accepted")
	}
	f.ImageGenProvider = "unknown"
	if err := SaveWeb(root, f); err == nil {
		t.Fatal("invalid file accepted")
	}
}

func TestWebImageGenSharesSearchLock(t *testing.T) {
	root := t.TempDir()
	if err := SaveWeb(root, WebFile{Fetch: WebBackend{Backend: "http", BaseURL: "http://example.test"}}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := SetWebImageGen(root, true, "xai", "local"); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			if err := SetWebSearch(root, false, "local", "ddg"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	f, err := LoadWeb(root)
	if err != nil || !f.ImageGenOn() || f.ImageGenProvider != "xai" || f.SearchOn() || f.SearchMode != "local" || f.Fetch.BaseURL != "http://example.test" {
		t.Fatalf("lost update: %+v %v", f, err)
	}
}
