package web

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/tools"
	"xbot/internal/work"
)

// Pasted images.
//
// A picture the owner pastes or drops into the composer is uploaded before the
// message is sent, so the message only carries the file name. The bytes go into
// the bot's own workspace, where view_image already resolves them, and never
// into the transcript: the line holds the name, so a reloaded page still finds
// the file and the model can open it later by path. That is the same rule a
// tool result's images follow: the bytes travel only where they are needed.
const (
	// maxPastedBytes caps one upload. view_image refuses anything over 10 MiB.
	maxPastedBytes = work.PastedMaxBytes
	// maxPastedPerMessage caps pictures on one message, matching the model's
	// four images per turn.
	maxPastedPerMessage = work.PastedMaxPerMessage
	// maxPastedTotalBytes caps what they weigh together, so the turn never has
	// to drop a picture the message asked for.
	maxPastedTotalBytes = work.PastedMaxTotalBytes
)

// postPaste stores one pasted image and returns its name.
func (s *Server) postPaste(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPastedBytes+1024)
	data, err := io.ReadAll(io.LimitReader(r.Body, maxPastedBytes+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "图片没传上来")
		return
	}
	if len(data) == 0 {
		writeErr(w, http.StatusBadRequest, "图片是空的")
		return
	}
	if len(data) > maxPastedBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, "图片超过 10 MB")
		return
	}
	mime := bot.SniffImage(data)
	if work.PastedExt(mime) == "" {
		writeErr(w, http.StatusBadRequest, "只认 PNG、JPEG、GIF、WebP")
		return
	}
	// The pixel cap is the model's, not the browser's. A stored picture is what
	// every later view_image reads back, and view_image only downscales the run
	// that opens it, so an oversized original is refused here instead of being
	// kept. Small enough is stored byte for byte.
	if iw, ih, fits := tools.FitsModel(mime, data); !fits {
		if iw == 0 && ih == 0 {
			writeErr(w, http.StatusBadRequest, "这张图读不出尺寸，换一张")
		} else {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("这张图 %d×%d，长边超过 %d 像素，先缩小再贴", iw, ih, tools.MaxImageEdge))
		}
		return
	}
	id, err := work.PastedName(mime)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := work.WritePasted(work.BotDir(s.Root, b.Name()), id, data); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"image": id})
}

// pastedProblem turns a view_image refusal into words the owner can act on.
func pastedProblem(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "pixel cap"), strings.Contains(msg, "cannot be downscaled"):
		return "这张图太大，先缩小再贴"
	case strings.Contains(msg, "cannot read image dimensions"), strings.Contains(msg, "cannot downscale"):
		return "这张图读不出尺寸，换一张"
	case strings.Contains(msg, "larger than 10 MiB"):
		return "图片超过 10 MB"
	}
	return "这张图用不了"
}

// getPaste serves a stored picture back to the chat and the composer preview.
func (s *Server) getPaste(w http.ResponseWriter, r *http.Request) {
	if !s.require(w, r) {
		return
	}
	b, err := s.owned(r.PathValue("name"))
	if err != nil {
		writeMissing(w, err)
		return
	}
	data, err := work.ReadPasted(work.BotDir(s.Root, b.Name()), r.PathValue("image"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	mime := bot.SniffImage(data)
	if mime == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// An id never changes contents, so the browser may keep it.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	_, _ = w.Write(data)
}

// checkPasted validates the image names a message carries and returns them in
// order. Each name must be one this server stored, so a message cannot point
// at an arbitrary file; the count and the bytes are capped before the turn
// starts, because a model call takes four images per turn.
func (s *Server) checkPasted(b *bot.Bot, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > maxPastedPerMessage {
		return nil, fmt.Errorf("一次最多 %d 张图", maxPastedPerMessage)
	}
	dir := work.BotDir(s.Root, b.Name())
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	total := 0
	for _, id := range ids {
		// PastedSize goes through the workspace root, so a name that resolves
		// outside it is refused here as well as at read time.
		size, err := work.PastedSize(dir, id)
		if err != nil {
			return nil, errPastedMissing
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		total += size
		if total > maxPastedTotalBytes {
			return nil, errPastedTooBig
		}
		out = append(out, id)
	}
	return out, nil
}

// errPastedMissing is one refusal for a name that is not a stored picture.
// It says the same thing whether the name was malformed or the file is gone.
var errPastedMissing = errors.New("图片已失效，重新贴一次")

// errPastedTooBig is one refusal for pictures that fit one by one but not
// together, the case where sending them would silently drop some.
var errPastedTooBig = errors.New("这几张图加起来超过 10 MB，去掉几张再发")
