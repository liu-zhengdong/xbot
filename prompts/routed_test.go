package prompts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRoutedModelIDMapsUnderProvider checks a model id with slashes, as routed
// ids like group/auto-deepseek-v4-1-flash use, maps to a nested overlay file
// instead of being refused.
func TestRoutedModelIDMapsUnderProvider(t *testing.T) {
	rel, err := Rel("group", "auto-deepseek-v4-1-flash/nested")
	if err != nil {
		t.Fatal(err)
	}
	if want := "models/group/auto-deepseek-v4-1-flash/nested.md"; rel != want {
		t.Fatalf("rel %q, want %q", rel, want)
	}
	root := t.TempDir()
	full := filepath.Join(root, "prompts", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("  Routed overlay.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(root, "group", "auto-deepseek-v4-1-flash/nested")
	if err != nil || got != "Routed overlay." {
		t.Fatalf("%q %v", got, err)
	}
}

// TestRoutedModelIDMissingOverlayIsEmpty checks an unknown routed model is an
// empty overlay, not an error and not a stray read.
func TestRoutedModelIDMissingOverlayIsEmpty(t *testing.T) {
	got, err := Load(t.TempDir(), "group", "auto-deepseek-v4-1-flash")
	if err != nil || got != "" {
		t.Fatalf("%q %v", got, err)
	}
}

// TestModelIDCannotEscapeModels pins the guard: no model id may walk out of the
// models tree, whatever slashes or dots it carries.
func TestModelIDCannotEscapeModels(t *testing.T) {
	bad := []string{
		"../escape", "a/../../escape", "/etc/passwd", "a//b", "/", "a/", "a/./b",
		"..", "a/..", `a\b`, "models/../../x", "a/../b", ".../x", "a/.../b",
		"a/b\x00c", "café/x",
	}
	for _, model := range bad {
		rel, err := Rel("deepseek", model)
		if err == nil {
			t.Errorf("model %q accepted as %q", model, rel)
			continue
		}
		if !strings.Contains(err.Error(), "bad model id") {
			t.Errorf("model %q error %v, want a bad-model-id error", model, err)
		}
	}
	for _, provider := range []string{"..", "a/b", "", ".", `a\b`, "/abs"} {
		if rel, err := Rel(provider, "m"); err == nil {
			t.Errorf("provider %q accepted as %q", provider, rel)
		}
	}
}

// TestRoutedPathsStayInsideRoot writes an overlay above the prompts tree and
// checks no model id can reach it.
func TestRoutedPathsStayInsideRoot(t *testing.T) {
	root := t.TempDir()
	secret := filepath.Join(root, "outside.md")
	if err := os.WriteFile(secret, []byte("SECRET"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"../../outside", "a/../../../outside"} {
		if rel, err := Rel("deepseek", model); err == nil {
			t.Fatalf("model %q resolved to %q", model, rel)
		}
		if got, err := Load(root, "deepseek", model); err == nil && strings.Contains(got, "SECRET") {
			t.Fatalf("model %q read outside the models tree", model)
		}
	}
}
