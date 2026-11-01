package web

import (
	"os"
	"path/filepath"
	"testing"
	"xbot/internal/bot"
)

func TestProfileColdRenamePreservesPeerFailureAsCommittedPartial(t *testing.T) {
	root := t.TempDir()
	previous := "cold-original"
	if _, err := bot.Create(root, previous, "duties", nil); err != nil {
		t.Fatal(err)
	}
	// There is no cached Kernel or Resources for this Bot. The original peer
	// membership operation must still run after the actual filesystem move.
	if err := os.WriteFile(filepath.Join(root, "groups"), []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	server := &Server{Root: root}
	next, description := "cold-current", "saved before peer failure"
	change, err := bot.ValidateProfileChange(bot.ProfileChange{Name: &next, Description: &description})
	if err != nil {
		t.Fatal(err)
	}
	value, err := server.applyProfileRuntime(previous, change)
	if err == nil || value.Error == "" {
		t.Fatalf("peer failure omitted: %+v %v", value, err)
	}
	if value.Name != next || value.PreviousName != previous || !value.Moved || !value.Applied {
		t.Fatalf("committed result lost: %+v %v", value, err)
	}
	saved, err := bot.Load(root, next)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Config().Description != description {
		t.Fatal(saved.Config())
	}
	if _, err := os.Stat(bot.Dir(root, previous)); !os.IsNotExist(err) {
		t.Fatalf("old directory revived: %v", err)
	}
}
