// Package work is the local workspace for a bot's shell and file tools.
package work

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Pasted images.
//
// A picture the owner pastes into the composer is uploaded before the message
// is sent. The bytes go here, in the bot's own workspace, and the transcript
// records only the file name: the file outlives the message, view_image already
// resolves it, and a reloaded page still finds it. Keeping the bytes out of the
// transcript is the same rule tool images follow.
const (
	// PastedDir is the directory under the bot workspace holding them.
	PastedDir = "paste"
	// PastedMaxBytes caps one upload. view_image refuses anything over 10 MiB,
	// so a bigger picture would be stored and then be unusable.
	PastedMaxBytes = 10 << 20
	// PastedMaxPerMessage caps pictures on one message. A model call accepts
	// four images per turn; a fifth would be dropped with a note, so the
	// composer refuses it first and says why.
	PastedMaxPerMessage = 4
	// PastedMaxTotalBytes caps what one message's pictures weigh together, the
	// same 10 MiB a model call takes. Counted at the message, not per file,
	// because four 9 MiB pictures would otherwise be accepted and then trimmed
	// silently to one.
	PastedMaxTotalBytes = 10 << 20
)

// ErrBadPastedID means a name is not one PastedName makes.
var ErrBadPastedID = errors.New("work: bad pasted image id")

// pastedExts are the extensions PastedName produces, by sniffed type.
var pastedExts = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// PastedExt maps an image MIME to the extension the file is stored under.
// Empty means the type is not one this package stores.
func PastedExt(mime string) string { return pastedExts[mime] }

// PastedName makes a new file name for an uploaded image of type mime.
// The name is random, so two pastes never overwrite each other.
func PastedName(mime string) (string, error) {
	ext := PastedExt(mime)
	if ext == "" {
		return "", ErrBadPastedID
	}
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]) + ext, nil
}

// ValidPastedID reports whether id is the shape PastedName makes. The name
// arrives in a request and becomes a path element, so the shape is checked
// rather than the path joined: no separator, no dot but the known extension.
func ValidPastedID(id string) bool {
	for _, ext := range pastedExts {
		if base, ok := strings.CutSuffix(id, ext); ok {
			if len(base) != 32 {
				return false
			}
			_, err := hex.DecodeString(base)
			return err == nil
		}
	}
	return false
}

// PastedPath resolves one stored picture. botDir is the bot workspace, and id
// must be a name PastedName made; anything else reports os.ErrNotExist so a
// caller cannot tell a bad name from a missing file.
//
// A name of the right shape says nothing about what it points at. The paste
// directory must be a real directory in this bot's own workspace, and the file
// must be a regular file: a link at either step could read a file out of, or
// write one into, a directory this bot does not own.
func PastedPath(botDir, id string) (string, error) {
	if botDir == "" || !ValidPastedID(id) {
		return "", os.ErrNotExist
	}
	dir, err := realPastedDir(botDir)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, id)
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() {
		return "", os.ErrNotExist
	}
	return path, nil
}

// ReadPasted returns one stored picture's bytes, capped at PastedMaxBytes.
// The file is opened without following a link, so the check above and this read
// cannot be separated by a swap, and the size is taken from the open file, so a
// picture grown past the cap after upload is refused instead of loaded.
func ReadPasted(botDir, id string) ([]byte, error) {
	path, err := PastedPath(botDir, id)
	if err != nil {
		return nil, err
	}
	f, err := openPastedFile(path)
	if err != nil {
		return nil, os.ErrNotExist
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return nil, os.ErrNotExist
	}
	if st.Size() > PastedMaxBytes {
		return nil, os.ErrNotExist
	}
	buf, err := io.ReadAll(io.LimitReader(f, PastedMaxBytes+1))
	if err != nil || len(buf) > PastedMaxBytes {
		return nil, os.ErrNotExist
	}
	return buf, nil
}

// realPastedDir is the paste directory of a bot workspace, checked to be a real
// directory. The workspace is resolved first, so a workspace reached through a
// link still finds its own paste directory.
func realPastedDir(botDir string) (string, error) {
	realBot, err := filepath.EvalSymlinks(botDir)
	if err != nil {
		return "", os.ErrNotExist
	}
	dir := filepath.Join(realBot, PastedDir)
	st, err := os.Lstat(dir)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return "", os.ErrNotExist
	}
	return dir, nil
}

// EnsurePastedDir creates the paste directory and refuses one that is a link.
// A bot's own shell can write its workspace, so the directory a picture is
// stored in must be a real directory: a link there would send the file wherever
// it points.
func EnsurePastedDir(botDir string) (string, error) {
	if botDir == "" {
		return "", os.ErrNotExist
	}
	realBot, err := filepath.EvalSymlinks(botDir)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(realBot, PastedDir)
	if st, err := os.Lstat(dir); err == nil {
		if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
			return "", os.ErrPermission
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

// PastedDirFor is the directory holding a bot workspace's pasted pictures.
func PastedDirFor(botDir string) string { return filepath.Join(botDir, PastedDir) }
