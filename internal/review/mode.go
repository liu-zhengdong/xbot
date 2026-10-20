package review

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"xbot/internal/fsutil"
)

// Whether the permission table asks the owner at all is the owner's call
// (ADR 0019). By default it does not: XBot is the owner's own tool, and every
// card is a pause they did not ask for. `xbot permissions on` turns the table
// back on for the whole install.

// ModePath is <root>/permissions.json.
func ModePath(root string) string { return filepath.Join(root, "permissions.json") }

type modeFile struct {
	// Confirm is "table" to ask by the permission table, or "off" (the
	// default) to ask only where a card is an input or an explicit setting.
	Confirm string `json:"confirm"`
}

// ConfirmTable reports whether calls are confirmed by the permission table.
// A missing or unreadable file means off.
func ConfirmTable(root string) bool {
	raw, err := os.ReadFile(ModePath(root))
	if err != nil {
		return false
	}
	var f modeFile
	if json.Unmarshal(raw, &f) != nil {
		return false
	}
	return f.Confirm == "table"
}

// SetConfirmTable turns the permission table's cards on or off.
func SetConfirmTable(root string, on bool) error {
	if root == "" {
		return errors.New("review: no root")
	}
	f := modeFile{Confirm: "off"}
	if on {
		f.Confirm = "table"
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(ModePath(root), append(raw, '\n'), 0o600)
}
