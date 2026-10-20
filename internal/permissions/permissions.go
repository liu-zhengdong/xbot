// Package permissions holds the install-wide switch for whether XBot asks the
// owner before acting (ADR 0019). Off, the default, nothing asks by
// permission: not the permission table, not the machine allow/ask/off
// switch, not the guard on turns from outside touching memory.
package permissions

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"xbot/internal/fsutil"
)

// Path is <root>/permissions.json.
func Path(root string) string { return filepath.Join(root, "permissions.json") }

type file struct {
	// Confirm is "table" to ask by the permission table and the machine
	// switch, or "off" (the default).
	Confirm string `json:"confirm"`
}

// On reports whether permission checks ask the owner. A missing or
// unreadable file means off.
func On(root string) bool {
	if root == "" {
		return false
	}
	raw, err := os.ReadFile(Path(root))
	if err != nil {
		return false
	}
	var f file
	if json.Unmarshal(raw, &f) != nil {
		return false
	}
	return f.Confirm == "table"
}

// Set turns permission checks on or off.
func Set(root string, on bool) error {
	if root == "" {
		return errors.New("permissions: no root")
	}
	f := file{Confirm: "off"}
	if on {
		f.Confirm = "table"
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(Path(root), append(raw, '\n'), 0o600)
}
