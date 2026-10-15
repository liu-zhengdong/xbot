package prompts

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// MaxBytes is how much overlay text is kept. A longer file is cut at this size.
const MaxBytes = 8 << 10

// Load returns the overlay for provider/model.
// A file at <root>/prompts/models/<provider>/<model>.md wins over the embedded copy.
// A missing file is an empty overlay, not an error.
func Load(root, provider, model string) (string, error) {
	rel, err := Rel(provider, model)
	if err != nil {
		return "", err
	}
	if root != "" {
		raw, err := os.ReadFile(filepath.Join(root, "prompts", rel))
		if err == nil {
			return clip(string(raw)), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	raw, err := Files.ReadFile(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return clip(string(raw)), nil
}

// Rel is models/<provider>/<model>.md. The two names cannot be paths.
func Rel(provider, model string) (string, error) {
	if !safeName(provider) || !safeName(model) {
		return "", fmt.Errorf("prompt: bad model id %q/%q", provider, model)
	}
	return filepath.Join("models", provider, model+".md"), nil
}

func safeName(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	for _, r := range s {
		if r > unicode.MaxASCII || (!unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' && r != '.') {
			return false
		}
	}
	return !strings.Contains(s, "..")
}

func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= MaxBytes {
		return s
	}
	return strings.TrimSpace(s[:MaxBytes]) + "\n[truncated]"
}
