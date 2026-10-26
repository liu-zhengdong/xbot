package work

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"xbot/internal/fsutil"
)

// Generated images.
//
// A picture the model's vendor-hosted image_generation tool returns arrives as
// base64 inside the response. The bytes go here, in the bot's own workspace,
// and the conversation records only the relative path: the bot looks at the
// files, keeps the good ones and hands them on itself. Nothing here reaches a
// chat bubble or a transcript Images field.
const (
	// GeneratedDir is the directory under the bot workspace holding them,
	// apart from paste/ so owner uploads and model output never mix.
	GeneratedDir = "generated"
	// GeneratedMaxBytes caps one decoded picture.
	GeneratedMaxBytes = 20 << 20
	// GeneratedMaxFiles caps how many pictures the directory may hold. A bot
	// that keeps generating without clearing out is told so instead of
	// filling the disk.
	GeneratedMaxFiles = 500
)

// ErrGeneratedFull means the directory already holds GeneratedMaxFiles entries.
var ErrGeneratedFull = fmt.Errorf("work: %s/ already holds %d files; delete some first", GeneratedDir, GeneratedMaxFiles)

// WriteGenerated stores one generated picture under a fresh random name and
// returns its path relative to the workspace, such as generated/<hex>.png.
// The type is sniffed from the bytes; anything that is not png, jpeg, gif or
// webp is refused. The file is written through the workspace root and created
// exclusively, so it cannot land outside the workspace or replace another one.
func WriteGenerated(botDir string, data []byte) (string, error) {
	if botDir == "" {
		return "", os.ErrNotExist
	}
	if len(data) == 0 {
		return "", errors.New("work: empty generated image")
	}
	if len(data) > GeneratedMaxBytes {
		return "", fmt.Errorf("work: generated image is %d bytes, over %d", len(data), GeneratedMaxBytes)
	}
	mime := http.DetectContentType(data)
	if PastedExt(mime) == "" {
		return "", fmt.Errorf("work: generated data is %s, not an image", mime)
	}
	if err := os.MkdirAll(botDir, 0700); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(botDir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	// Serialize the count and exclusive create across processes as well as
	// concurrent foreground/background requests for this Bot.
	lock, err := root.OpenFile(GeneratedDir+".lock", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if errors.Is(err, os.ErrExist) {
		lock, err = root.OpenFile(GeneratedDir+".lock", os.O_RDWR, 0600)
	}
	if err != nil {
		return "", err
	}
	defer lock.Close()
	if err := fsutil.LockFile(lock, true, true); err != nil {
		return "", err
	}
	defer fsutil.UnlockFile(lock)
	if err := root.MkdirAll(GeneratedDir, 0700); err != nil {
		return "", err
	}
	dir, err := root.Open(GeneratedDir)
	if err != nil {
		return "", err
	}
	names, err := dir.Readdirnames(GeneratedMaxFiles)
	dir.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if err == nil && len(names) >= GeneratedMaxFiles {
		return "", ErrGeneratedFull
	}
	name, err := PastedName(mime)
	if err != nil {
		return "", err
	}
	rel := GeneratedDir + "/" + name
	f, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		root.Remove(rel)
		return "", err
	}
	if err := f.Close(); err != nil {
		root.Remove(rel)
		return "", err
	}
	return rel, nil
}
