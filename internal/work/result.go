package work

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
)

const ResultDir = "results"
const ResultMaxBytes = 20 << 20

// WriteResult stores bounded local tool output under an exclusive random name.
// ext is an internal format choice, never a remote filename.
func WriteResult(workspace string, data []byte, ext string) (string, error) {
	if workspace == "" {
		return "", os.ErrNotExist
	}
	switch ext {
	case "txt", "json", "bin":
	default:
		return "", errors.New("work: unsupported result format")
	}
	if len(data) > ResultMaxBytes {
		return "", fmt.Errorf("work: result is %d bytes, over %d", len(data), ResultMaxBytes)
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	if err := os.MkdirAll(workspace, 0700); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if err := root.MkdirAll(ResultDir, 0700); err != nil {
		return "", err
	}
	rel := ResultDir + "/" + hex.EncodeToString(id[:]) + "." + ext
	f, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	n, writeErr := f.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	if err := errors.Join(writeErr, f.Close()); err != nil {
		_ = root.Remove(rel)
		return "", err
	}
	return rel, nil
}
