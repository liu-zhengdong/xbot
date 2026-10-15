package tools

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Resolve cleans path, makes it absolute against the working directory,
// and evaluates symlinks on the longest existing prefix so a confirmation
// shows the real target.
func Resolve(path string) (string, error) {
	if path == "" || strings.ContainsRune(path, 0) {
		return "", errors.New("invalid path")
	}
	if !filepath.IsAbs(path) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		path = filepath.Join(cwd, path)
	}
	return evalExisting(filepath.Clean(path))
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
