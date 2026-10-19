// Package work is the local workspace for a bot's shell and file tools.
package work

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
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
func PastedPath(botDir, id string) (string, error) {
	if botDir == "" || !ValidPastedID(id) {
		return "", os.ErrNotExist
	}
	return filepath.Join(botDir, PastedDir, id), nil
}

// PastedDirFor is the directory holding a bot workspace's pasted pictures.
func PastedDirFor(botDir string) string { return filepath.Join(botDir, PastedDir) }
