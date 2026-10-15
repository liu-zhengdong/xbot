// Package machine is the per-machine execution switch: allow, ask, or off.
// A missing entry is allow, which keeps today's shell behavior.
package machine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"xbot/internal/fsutil"
)

const (
	// Allow keeps the tool's own confirm rules.
	Allow = "allow"
	// Ask requires the owner to confirm every execution, including read-only commands.
	Ask = "ask"
	// Off refuses execution.
	Off = "off"
	// Local is this machine.
	Local = "local"
)

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

type file struct {
	Machines map[string]string `json:"machines"`
}

// Path is <root>/machines.json.
func Path(root string) string { return filepath.Join(root, "machines.json") }

// Get returns the effective mode. An unset machine is allow.
func Get(root, name string) (string, error) {
	name, err := clean(name)
	if err != nil {
		return "", err
	}
	modes, err := load(root)
	if err != nil {
		return "", err
	}
	mode := modes[name]
	if mode == "" {
		return Allow, nil
	}
	if !valid(mode) {
		return "", fmt.Errorf("machine: bad mode %q", mode)
	}
	return mode, nil
}

// Set stores a mode for one machine.
func Set(root, name, mode string) error {
	name, err := clean(name)
	if err != nil {
		return err
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if !valid(mode) {
		return fmt.Errorf("machine: mode must be allow, ask, or off")
	}
	modes, err := load(root)
	if err != nil {
		return err
	}
	if modes == nil {
		modes = map[string]string{}
	}
	modes[name] = mode
	return save(root, modes)
}

// List returns stored modes. Unset machines are omitted; callers treat those as allow.
func List(root string) (map[string]string, error) {
	return load(root)
}

func clean(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = Local
	}
	if !namePattern.MatchString(name) {
		return "", fmt.Errorf("machine: bad name %q", name)
	}
	return name, nil
}

func valid(mode string) bool {
	switch mode {
	case Allow, Ask, Off:
		return true
	default:
		return false
	}
}

func load(root string) (map[string]string, error) {
	raw, err := os.ReadFile(Path(root))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var doc file
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if doc.Machines == nil {
		doc.Machines = map[string]string{}
	}
	return doc.Machines, nil
}

func save(root string, modes map[string]string) error {
	raw, err := fsutil.Marshal(file{Machines: modes})
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(Path(root), raw, 0600)
}
