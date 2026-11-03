package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbot/internal/cliops"
)

func TestRuntimeSourceCanonicalRootAndExplicitSelection(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	canonical, err := cliops.CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XBOT_ROOT", alias)
	t.Setenv("XBOT_ENDPOINT", "http://127.0.0.1:12345")
	t.Setenv("XBOT_BOT", "obsolete-diagnostic")
	for _, raw := range []string{root, alias} {
		given, endpoint, err := runtimeSource("example", raw, true)
		if err != nil || given != canonical || endpoint != "http://127.0.0.1:12345" {
			t.Fatalf("source root=%q endpoint=%q err=%v", given, endpoint, err)
		}
	}
	// An implicit CLI default is not a source identity or an offline fallback.
	given, _, err := runtimeSource("example", "/absent/default", false)
	if err != nil || given != canonical {
		t.Fatalf("implicit default changed source: root=%q err=%v", given, err)
	}
	for _, tc := range []struct {
		raw, problem string
	}{
		{"", "explicit --root must name"},
		{" \t", "explicit --root must name"},
		{t.TempDir(), "runtime data root mismatch"},
		{filepath.Join(t.TempDir(), "absent"), "explicit root:"},
	} {
		_, _, err := runtimeSource("example", tc.raw, true)
		if err == nil || !strings.Contains(err.Error(), "example: "+tc.problem) {
			t.Fatalf("explicit root=%q err=%v", tc.raw, err)
		}
	}
}

func TestRuntimeSourceMissingEnvironment(t *testing.T) {
	t.Setenv("XBOT_ENDPOINT", "")
	for _, root := range []string{"", " \t"} {
		t.Setenv("XBOT_ROOT", root)
		if _, _, err := runtimeSource("example", "", false); err == nil || !strings.Contains(err.Error(), "requires XBOT_ROOT") {
			t.Fatalf("root=%q err=%v", root, err)
		}
	}
	t.Setenv("XBOT_ROOT", filepath.Join(t.TempDir(), "absent"))
	if _, _, err := runtimeSource("example", "", false); err == nil || !strings.Contains(err.Error(), "runtime root:") {
		t.Fatalf("missing runtime root: %v", err)
	}
	root := t.TempDir()
	t.Setenv("XBOT_ROOT", root)
	if _, _, err := runtimeSource("example", "", false); err == nil || !strings.Contains(err.Error(), "requires an active local XBot endpoint") {
		t.Fatalf("missing endpoint: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("source selection initialized files: entries=%v err=%v", entries, err)
	}
}
