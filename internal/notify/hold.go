package notify

import (
	"encoding/json"
	"errors"
	"os"
	"strings"

	"xbot/internal/fsutil"
)

type holdFile struct {
	Notes []string `json:"notes"`
}

// Held returns notes waiting out quiet hours. A missing file is empty.
func Held(path string) ([]string, error) {
	return readHeld(path)
}

// Hold appends a notification that should wait until quiet hours end.
func Hold(path, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("notify: empty note")
	}
	return fsutil.WithLock(path+".lock", func() error {
		notes, err := readHeld(path)
		if err != nil {
			return err
		}
		notes = append(notes, text)
		return writeHeld(path, notes)
	})
}

// TakeHeld returns held notes and clears the file. A missing file is empty.
func TakeHeld(path string) ([]string, error) {
	var notes []string
	err := fsutil.WithLock(path+".lock", func() error {
		var err error
		notes, err = readHeld(path)
		if err != nil || len(notes) == 0 {
			return err
		}
		return os.Remove(path)
	})
	return notes, err
}

// IsBatch reports a note marked as a quiet-hours batch on the record.
// Text that merely starts with the batch heading is not a batch.
func IsBatch(n Note) bool {
	return n.Batch
}

// BatchBody is one outbox note covering every held line.
func BatchBody(notes []string) string {
	var b strings.Builder
	b.WriteString("Quiet hours batch:")
	for _, n := range notes {
		n = strings.TrimSpace(strings.ReplaceAll(n, "\n", " "))
		if n == "" {
			continue
		}
		b.WriteString("\n- ")
		b.WriteString(n)
	}
	return b.String()
}

func readHeld(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f holdFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	return f.Notes, nil
}

func writeHeld(path string, notes []string) error {
	raw, err := fsutil.Marshal(holdFile{Notes: notes})
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(path, raw, 0600)
}
