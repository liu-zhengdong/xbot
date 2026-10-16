package prompts

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
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

// Rel is models/<provider>/<model>.md. A model id may carry slashes, as routed
// ids like group/auto-deepseek-v4-1-flash do; each segment is then a directory
// level under the provider. Neither name may escape the models tree.
func Rel(provider, model string) (string, error) {
	if !safeName(provider) {
		return "", fmt.Errorf("prompt: bad provider %q", provider)
	}
	segs, err := modelPath(model)
	if err != nil {
		return "", err
	}
	parts := append([]string{"models", provider}, segs...)
	last := len(parts) - 1
	parts[last] += ".md"
	return path.Join(parts...), nil
}

// modelPath splits a model id into safe path segments. A slash only separates
// segments; empty, "." and ".." segments, backslashes and any character outside
// the safe set are refused, so no model id can point outside models/.
func modelPath(model string) ([]string, error) {
	bad := func() ([]string, error) {
		return nil, fmt.Errorf("prompt: bad model id %q", model)
	}
	if model == "" {
		return bad()
	}
	var out []string
	for _, seg := range strings.Split(model, "/") {
		if !safeName(seg) {
			return bad()
		}
		out = append(out, seg)
	}
	return out, nil
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
