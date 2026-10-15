package prompts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMissingOverlayIsEmpty(t *testing.T) {
	got, err := Load(t.TempDir(), "deepseek", "deepseek-chat")
	if err != nil || got != "" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := Load(t.TempDir(), "..", "x"); err == nil {
		t.Fatal("path traversal")
	}
	if _, err := Rel("deepseek", "chat/extra"); err == nil {
		t.Fatal("slash in model")
	}
}

func TestOverrideWins(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "prompts", "models", "deepseek", "deepseek-chat.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("  Prefer short tool calls.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(root, "deepseek", "deepseek-chat")
	if err != nil || got != "Prefer short tool calls." {
		t.Fatalf("%q %v", got, err)
	}
}

func TestEmbeddedFilesAreNotHoldoutCases(t *testing.T) {
	banned := []string{
		"SHIPPED.txt", "CHECKLIST.md", "Scale", "plot.svg", "blank-chart",
		"the chart is blank", "meter.txt", "proactivity-holdout",
	}
	entries, err := Files.ReadDir("models")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		raw, err := Files.ReadFile("models/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		body := string(raw)
		for _, s := range banned {
			if strings.Contains(body, s) {
				t.Fatalf("%s contains %q", e.Name(), s)
			}
		}
	}
}
