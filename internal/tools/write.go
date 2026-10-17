package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"xbot/internal/fsutil"
)

const maxWriteBytes = 1 << 20

// WriteFile replaces a file's contents. The write is atomic.
// Root is the base for relative paths. Empty uses the working directory.
// Sandbox, when set, rejects any path that resolves outside it.
type WriteFile struct {
	Root    string
	Sandbox string
	rootBind
}

func (w *WriteFile) Name() string { return "write_file" }

func (w *WriteFile) Description() string {
	return "Create or replace a file with the given content. Parent directories are created."
}

func (w *WriteFile) Parameters() any {
	return objectSchema(map[string]any{
		"path":    stringProp("File path"),
		"content": stringProp("Full new contents of the file"),
	}, []string{"path", "content"})
}

type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Confirm only builds the card summary. The kernel review path decides
// whether the write needs the owner; it calls ResolvedPath to check for a
// persona or profile self-edit.
func (w *WriteFile) Confirm(args json.RawMessage) (bool, string, error) {
	return w.ConfirmContext(context.Background(), args)
}

func (w *WriteFile) ConfirmContext(ctx context.Context, args json.RawMessage) (bool, string, error) {
	in, path, err := w.parse(ctx, args)
	if err != nil {
		return false, "", err
	}
	return false, fmt.Sprintf("write %s (%d bytes)", DisplayPath(w.Sandbox, path), len(in.Content)), nil
}

// ResolvedPath reports the absolute path this call would write, so the kernel
// can decide whether it is a persona or profile self-edit. It uses the
// foreground directory. ResolvedPathContext uses the caller's directory.
func (w *WriteFile) ResolvedPath(args json.RawMessage) string {
	return w.ResolvedPathContext(context.Background(), args)
}

func (w *WriteFile) ResolvedPathContext(ctx context.Context, args json.RawMessage) string {
	_, path, err := w.parse(ctx, args)
	if err != nil {
		return ""
	}
	return path
}

// ShownPath is the path the model should see. A sandbox rewrites the host path.
func (w *WriteFile) ShownPath(abs string) string { return DisplayPath(w.Sandbox, abs) }

func (w *WriteFile) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := machineOff(); err != nil {
		return "", err
	}
	in, path, err := w.parse(ctx, args)
	if err != nil {
		return "", err
	}
	if err := refuseDir(path); err != nil {
		return "", fmt.Errorf("write_file: %s", RewriteHost(w.Sandbox, path, err.Error()))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", fmt.Errorf("write_file: %s", RewriteHost(w.Sandbox, path, err.Error()))
	}
	if err := fsutil.WriteAtomic(path, []byte(in.Content), 0644); err != nil {
		return "", fmt.Errorf("write_file: %s", RewriteHost(w.Sandbox, path, err.Error()))
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(in.Content), DisplayPath(w.Sandbox, path)), nil
}

func (w *WriteFile) parse(ctx context.Context, args json.RawMessage) (writeArgs, string, error) {
	var in writeArgs
	if err := json.Unmarshal(args, &in); err != nil {
		return writeArgs{}, "", fmt.Errorf("write_file: %w", err)
	}
	if len(in.Content) > maxWriteBytes {
		return writeArgs{}, "", fmt.Errorf("write_file: content exceeds %d bytes", maxWriteBytes)
	}
	path, err := ResolveRoot(w.rootBind.dir(ctx, w.Root), AliasCase(w.Sandbox, in.Path))
	if err != nil {
		return writeArgs{}, "", fmt.Errorf("write_file: %w", err)
	}
	if err := Confine(w.Sandbox, path); err != nil {
		return writeArgs{}, "", fmt.Errorf("write_file: %w", err)
	}
	return in, path, nil
}

func refuseDir(path string) error {
	st, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if st.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}
	return nil
}

// LogicalPathContext reports the absolute caller path without following symlinks.
func (w *WriteFile) LogicalPathContext(ctx context.Context, args json.RawMessage) string {
	var in struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(args, &in) != nil || in.Path == "" {
		return ""
	}
	path := AliasCase(w.Sandbox, in.Path)
	if !filepath.IsAbs(path) {
		path = filepath.Join(w.rootBind.dir(ctx, w.Root), path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	return abs
}
