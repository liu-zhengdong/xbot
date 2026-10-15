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
type EditFile struct {
	Root string
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

func (e *EditFile) Confirm(args json.RawMessage) (bool, string, error) {
	in, path, err := e.parse(args)
	if err != nil {
		return false, "", err
	}
	return true, fmt.Sprintf("edit %s (%d -> %d bytes)", path, len(in.OldString), len(in.NewString)), nil
}

func (e *EditFile) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	in, path, err := e.parse(args)
	if err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("edit_file: %w", err)
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return "", fmt.Errorf("edit_file: %w", err)
	}
	if st.IsDir() {
		_ = f.Close()
		return "", fmt.Errorf("edit_file: %s is a directory", path)
	}
	body, err := io.ReadAll(io.LimitReader(f, maxEditBytes+1))
	_ = f.Close()
	if err != nil {
		return "", fmt.Errorf("edit_file: %w", err)
	}
	if len(body) > maxEditBytes {
		return "", fmt.Errorf("edit_file: %s exceeds %d bytes", path, maxEditBytes)
	}
	s := string(body)
	n := strings.Count(s, in.OldString)
	if n == 0 {
		return "", fmt.Errorf("edit_file: old_string not found in %s", path)
	}
	if n > 1 {
		return "", fmt.Errorf("edit_file: old_string matches %d times in %s", n, path)
	}
	updated := strings.Replace(s, in.OldString, in.NewString, 1)
	mode := st.Mode().Perm()
	if mode == 0 {
		mode = 0644
	}
	if err := fsutil.WriteAtomic(path, []byte(updated), mode); err != nil {
		return "", fmt.Errorf("edit_file: %w", err)
	}
	return fmt.Sprintf("updated %s", path), nil
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
	path, err := ResolveRoot(e.Root, in.Path)
	if err != nil {
		return editArgs{}, "", fmt.Errorf("edit_file: %w", err)
	}
	return in, path, nil
}
