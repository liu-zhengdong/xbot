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

// pastedRoot opens the bot workspace as an os.Root. Everything below reads and
// writes through it, so every path is resolved inside the workspace by the
// kernel: a symlink in the paste directory, a paste directory that is itself a
// symlink, or a swap between a check and a read cannot reach outside it. Doing
// this by hand leaves exactly that gap.
func pastedRoot(botDir string) (*os.Root, error) {
	if botDir == "" {
		return nil, os.ErrNotExist
	}
	return os.OpenRoot(botDir)
}

// PastedPath resolves one stored picture to the path it will be read from. The
// returned path is for display and for tests; a caller that reads the bytes
// must use ReadPasted, which goes through the workspace root.
//
// id must be a name PastedName made. Anything else reports os.ErrNotExist, so a
// caller cannot tell a bad name from a missing file.
func PastedPath(botDir, id string) (string, error) {
	if botDir == "" || !ValidPastedID(id) {
		return "", os.ErrNotExist
	}
	root, err := pastedRoot(botDir)
	if err != nil {
		return "", os.ErrNotExist
	}
	defer root.Close()
	st, err := root.Stat(PastedDir + "/" + id)
	if err != nil || !st.Mode().IsRegular() {
		return "", os.ErrNotExist
	}
	real, err := filepath.EvalSymlinks(botDir)
	if err != nil {
		return "", os.ErrNotExist
	}
	return filepath.Join(real, PastedDir, id), nil
}

// ReadPasted returns one stored picture's bytes, capped at PastedMaxBytes. The
// file is read through the workspace root, so a link cannot redirect it, and
// the size is checked on the open file, so a picture grown past the cap after
// upload is refused instead of loaded into memory.
func ReadPasted(botDir, id string) ([]byte, error) {
	if botDir == "" || !ValidPastedID(id) {
		return nil, os.ErrNotExist
	}
	root, err := pastedRoot(botDir)
	if err != nil {
		return nil, os.ErrNotExist
	}
	defer root.Close()
	// Stat before opening: opening a FIFO waits for a writer, so a name swapped
	// for one would hang the request instead of being refused. The type is
	// checked again on the open file, which is what a read actually uses.
	// Stat before opening: opening a FIFO waits for a writer, so a name swapped
	// for one would hang the request instead of being refused. The type is
	// checked again on the open file, which is what a read actually uses.
	if st, err := root.Stat(PastedDir + "/" + id); err != nil || !st.Mode().IsRegular() {
		return nil, os.ErrNotExist
	}
	f, err := root.Open(PastedDir + "/" + id)
	if err != nil {
		return nil, os.ErrNotExist
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > PastedMaxBytes {
		return nil, os.ErrNotExist
	}
	buf, err := io.ReadAll(io.LimitReader(f, PastedMaxBytes+1))
	if err != nil || len(buf) > PastedMaxBytes {
		return nil, os.ErrNotExist
	}
	return buf, nil
}

// WritePasted stores one uploaded picture. It goes through the workspace root
// for the same reason ReadPasted does, so the file cannot land outside it.
func WritePasted(botDir, id string, data []byte) error {
	if botDir == "" || !ValidPastedID(id) {
		return os.ErrNotExist
	}
	root, err := pastedRoot(botDir)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.MkdirAll(PastedDir, 0700); err != nil {
		return err
	}
	return root.WriteFile(PastedDir+"/"+id, data, 0600)
}

// PastedDirFor is the directory holding a bot workspace's pasted pictures.
func PastedDirFor(botDir string) string { return filepath.Join(botDir, PastedDir) }

// PastedSize reports the size of one stored picture, checked the same way a
// read is. A caller that only needs the size uses this instead of stat-ing a
// path itself, which would follow a link.
func PastedSize(botDir, id string) (int, error) {
	if botDir == "" || !ValidPastedID(id) {
		return 0, os.ErrNotExist
	}
	root, err := pastedRoot(botDir)
	if err != nil {
		return 0, os.ErrNotExist
	}
	defer root.Close()
	st, err := root.Stat(PastedDir + "/" + id)
	if err != nil || !st.Mode().IsRegular() || st.Size() > PastedMaxBytes {
		return 0, os.ErrNotExist
	}
	return int(st.Size()), nil
}
