package bot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNonmigratingSkillSwitchPreservesLegacyStyle(t *testing.T) {
	root := t.TempDir()
	dir := Dir(root, "legacy")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	original := []byte(`{"name":"legacy","description":"unchanged","style":{"tone":"gentle"}}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := SetSkillDisabled(root, "legacy", "ship", true); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	var legacy struct {
		Tone string `json:"tone"`
	}
	if err := json.Unmarshal(got["style"], &legacy); err != nil || legacy.Tone != "gentle" {
		t.Fatalf("nonmigrating switch discarded legacy preference: %s (%v)", raw, err)
	}
	var disabled []string
	if err := json.Unmarshal(got["disabled_skills"], &disabled); err != nil {
		t.Fatal(err)
	}
	if len(disabled) != 1 || disabled[0] != "ship" {
		t.Fatalf("disabled=%v", disabled)
	}
	if _, err := os.Stat(filepath.Join(root, "memory")); !os.IsNotExist(err) {
		t.Fatalf("switch migrated memory: %v", err)
	}
}
