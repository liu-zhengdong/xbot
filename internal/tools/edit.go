package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"xbot/internal/fsutil"
)

const maxEditBytes = 2 << 20

// EditFile replaces one exact occurrence of old_string with new_string.
// Root is the base for relative paths. Empty uses the working directory.
// Sandbox, when set, rejects any path that resolves outside it.
type EditFile struct {
	Root    string
	Sandbox string
	rootBind
}

func (e *EditFile) Name() string { return "edit_file" }

func (e *EditFile) Description() string {
	return "Replace one exact occurrence of old_string with new_string. The match must be unique."
}

func (e *EditFile) Parameters() any {
	return objectSchema(map[string]any{
		"path":       stringProp("File path"),
		"old_string": stringProp("Exact text to replace. It must occur once."),
		"new_string": stringProp("Replacement text"),
	}, []string{"path", "old_string", "new_string"})
}

type editArgs struct {
	Path      string `json:"path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

// Confirm only builds the card summary. The kernel review path decides
// whether the edit needs the owner; it calls ResolvedPath to check for a
// persona or profile self-edit.
func (e *EditFile) Confirm(args json.RawMessage) (bool, string, error) {
	in, path, err := e.parse(args)
	if err != nil {
		return false, "", err
	}
	return false, fmt.Sprintf("edit %s (%d -> %d bytes)", DisplayPath(e.Sandbox, path), len(in.OldString), len(in.NewString)), nil
}

// ResolvedPath reports the absolute path this call would edit, so the kernel
// can decide whether it is a persona or profile self-edit.
func (e *EditFile) ResolvedPath(args json.RawMessage) string {
	_, path, err := e.parse(args)
	if err != nil {
		return ""
	}
	return path
}

// ShownPath is the path the model should see. A sandbox rewrites the host path.
func (e *EditFile) ShownPath(abs string) string { return DisplayPath(e.Sandbox, abs) }

func (e *EditFile) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := machineOff(); err != nil {
		return "", err
	}
	in, path, err := e.parse(args)
	if err != nil {
		return "", err
	}
	shown := DisplayPath(e.Sandbox, path)
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("edit_file: %s", RewriteHost(e.Sandbox, path, err.Error()))
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return "", fmt.Errorf("edit_file: %s", RewriteHost(e.Sandbox, path, err.Error()))
	}
	if st.IsDir() {
		_ = f.Close()
		return "", fmt.Errorf("edit_file: %s is a directory", shown)
	}
	body, err := io.ReadAll(io.LimitReader(f, maxEditBytes+1))
	_ = f.Close()
	if err != nil {
		return "", fmt.Errorf("edit_file: %s", RewriteHost(e.Sandbox, path, err.Error()))
	}
	if len(body) > maxEditBytes {
		return "", fmt.Errorf("edit_file: %s exceeds %d bytes", shown, maxEditBytes)
	}
	s := string(body)
	n := strings.Count(s, in.OldString)
	if n == 0 {
		return "", fmt.Errorf("edit_file: old_string not found in %s", shown)
	}
	if n > 1 {
		return "", fmt.Errorf("edit_file: old_string matches %d times in %s", n, shown)
	}
	updated := strings.Replace(s, in.OldString, in.NewString, 1)
	mode := st.Mode().Perm()
	if mode == 0 {
		mode = 0644
	}
	if err := fsutil.WriteAtomic(path, []byte(updated), mode); err != nil {
		return "", fmt.Errorf("edit_file: %s", RewriteHost(e.Sandbox, path, err.Error()))
	}
	return fmt.Sprintf("updated %s", shown), nil
}

func (e *EditFile) parse(args json.RawMessage) (editArgs, string, error) {
	var in editArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return editArgs{}, "", fmt.Errorf("edit_file: %w", err)
	}
	if in.OldString == "" {
		return editArgs{}, "", fmt.Errorf("edit_file: old_string is empty")
	}
	if in.OldString == in.NewString {
		return editArgs{}, "", fmt.Errorf("edit_file: old_string and new_string are identical")
	}
	path, err := ResolveRoot(e.rootBind.dir(e.Root), AliasCase(e.Sandbox, in.Path))
	if err != nil {
		return editArgs{}, "", fmt.Errorf("edit_file: %w", err)
	}
	if err := Confine(e.Sandbox, path); err != nil {
		return editArgs{}, "", fmt.Errorf("edit_file: %w", err)
	}
	return in, path, nil
}
