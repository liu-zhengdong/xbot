package bot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"xbot/internal/work"
)

func TestProfileChangePersistsCurrentCwdForColdProcess(t *testing.T) {
	root := t.TempDir()
	original, err := Create(root, "before", "duties", nil)
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(work.BotDir(root, "before"), "project")
	if err := os.MkdirAll(project, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(Dir(root, "before"), "cwd"), []byte(project+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	next := "after"
	current, result, err := original.ApplyProfileChange(normalizedChange(t, ProfileChange{Name: &next}))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Moved {
		t.Fatal(result)
	}
	// A cold process has only the new config, so the persisted file itself must
	// already identify the same project. No old identity registry is available.
	fresh, err := Load(root, next)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(Dir(root, fresh.Name()), "cwd"))
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(work.BotDir(root, next), "project")
	if strings.TrimSpace(string(data)) != expected {
		t.Fatalf("cold cwd %q want %q", data, expected)
	}
	if st, err := os.Stat(expected); err != nil || !st.IsDir() {
		t.Fatalf("same project missing: %v", err)
	}
	external := filepath.Join(root, "external-project")
	if err := os.Mkdir(external, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(Dir(root, next), "cwd"), []byte(external+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	final := "final"
	_, result, err = current.ApplyProfileChange(normalizedChange(t, ProfileChange{Name: &final}))
	if err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(filepath.Join(Dir(root, final), "cwd"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != external+"\n" {
		t.Fatalf("external cwd changed: %q %+v", data, result)
	}
	for _, previous := range []string{"before", "after"} {
		if _, err := os.Stat(Dir(root, previous)); !os.IsNotExist(err) {
			t.Fatalf("old directory %s: %v", previous, err)
		}
	}
}
