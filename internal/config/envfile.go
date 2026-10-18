package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"xbot/internal/fsutil"
)

// EnvFile is <root>/env, the KEY=VALUE file the desktop app loads at start.
func EnvFile(root string) string { return filepath.Join(root, "env") }

// SaveEnvKey sets name=value in <root>/env and leaves every other line as it
// was. An existing line for name (with or without "export ") is replaced; a
// missing file is created. The file is written atomically with mode 0600.
// Nothing is set in the process, user or system environment.
func SaveEnvKey(root, name, value string) error {
	if !envName.MatchString(name) {
		return fmt.Errorf("env: bad name %q", name)
	}
	if value == "" || strings.ContainsAny(value, " \t\r\n\"'#") {
		return errors.New("env: value must be one word without quotes or #")
	}
	raw, err := os.ReadFile(EnvFile(root))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var out []string
	done := false
	text := strings.TrimSuffix(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	for _, line := range strings.Split(text, "\n") {
		if text == "" {
			break
		}
		key, _, ok := strings.Cut(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(line, "\ufeff")), "export ")), "=")
		if ok && strings.TrimSpace(key) == name {
			if !done {
				out = append(out, name+"="+value)
				done = true
			}
			continue
		}
		out = append(out, line)
	}
	if !done {
		out = append(out, name+"="+value)
	}
	return fsutil.WriteAtomic(EnvFile(root), []byte(strings.Join(out, "\n")+"\n"), 0600)
}
