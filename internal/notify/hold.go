package notify

import (
	"encoding/json"
	"errors"
	"os"

	"xbot/internal/fsutil"
)

// TakeLegacyHeld returns the notes quiet hours used to keep in held.json and
// removes the file. A missing file is empty. Nothing writes this file anymore.
func TakeLegacyHeld(path string) ([]string, error) {
	var notes []string
	err := fsutil.WithLock(path+".lock", func() error {
		raw, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		var f struct {
			Notes []string `json:"notes"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			return err
		}
		notes = f.Notes
		return os.Remove(path)
	})
	_ = os.Remove(path + ".lock")
	return notes, err
}
