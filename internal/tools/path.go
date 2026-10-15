package tools

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Resolve cleans path against the working directory.
func Resolve(path string) (string, error) {
	return ResolveRoot("", path)
}

// ResolveRoot cleans path. Relative paths join root, or the working directory when root is empty.
// Absolute paths stay absolute. Symlinks on the longest existing prefix are evaluated
// so a confirmation shows the real target.
func ResolveRoot(root, path string) (string, error) {
	if path == "" || strings.ContainsRune(path, 0) {
		return "", errors.New("invalid path")
	}
	if !filepath.IsAbs(path) {
		base := root
		if base == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return "", err
			}
			base = cwd
		}
		path = filepath.Join(base, path)
	}
	return evalExisting(filepath.Clean(path))
}

// CaseAlias is the working directory inside an eval shell.
// File tools map it back onto the host sandbox.
const CaseAlias = "/sandbox"

// DisplayPath is the path shown to the model.
// With a sandbox it is /sandbox or /sandbox/<rel>. Otherwise path is unchanged.
func DisplayPath(sandbox, path string) string {
	if sandbox == "" || path == "" {
		return path
	}
	root := filepath.Clean(sandbox)
	if resolved, err := filepath.EvalSymlinks(sandbox); err == nil {
		root = resolved
	}
	rel, err := filepath.Rel(root, filepath.Clean(path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	if rel == "." {
		return CaseAlias
	}
	return CaseAlias + "/" + filepath.ToSlash(rel)
}

// RewriteHost replaces a host sandbox path in msg with its /sandbox form.
func RewriteHost(sandbox, path, msg string) string {
	if sandbox == "" || msg == "" {
		return msg
	}
	if path != "" {
		msg = strings.ReplaceAll(msg, path, DisplayPath(sandbox, path))
	}
	root := filepath.Clean(sandbox)
	if resolved, err := filepath.EvalSymlinks(sandbox); err == nil {
		root = resolved
	}
	if root != "" && root != CaseAlias {
		msg = strings.ReplaceAll(msg, root, CaseAlias)
	}
	return msg
}

// AliasCase rewrites a /sandbox path onto sandbox. Other paths are unchanged.
// An empty sandbox disables the rewrite.
func AliasCase(sandbox, path string) string {
	if sandbox == "" {
		return path
	}
	if path == CaseAlias {
		return sandbox
	}
	prefix := CaseAlias + "/"
	if strings.HasPrefix(path, prefix) {
		return filepath.Join(sandbox, strings.TrimPrefix(path, prefix))
	}
	return path
}

// Confine rejects path when it is outside root. Both must already be cleaned.
// Symlinks are the caller's job: pass the path from ResolveRoot, which evaluates them.
func Confine(root, path string) error {
	if root == "" {
		return nil
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, filepath.Clean(path))
	if err != nil {
		return errors.New("path is outside the workspace")
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("path is outside the workspace")
	}
	return nil
}

func evalExisting(path string) (string, error) {
	path = filepath.Clean(path)
	if _, err := os.Lstat(path); err == nil {
		return filepath.EvalSymlinks(path)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return path, nil
	}
	parent, err := evalExisting(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}
