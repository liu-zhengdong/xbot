package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/tools"
	"xbot/internal/work"
)

// Pictures the bot shows the owner.
//
// notify_owner takes images: pasted ids or workspace paths. They are stored in
// paste/ like the owner's own pictures, and the names wait on the turn until its
// final reply is recorded; that line carries them in Entry.Images, so the chat
// draws them in the reply bubble. The bytes never enter the transcript. The
// caps are the owner's: four pictures and 10 MiB per message, counted over every
// call in the turn, because they all land on the same reply.

// outPicture is one picture checked and ready to store.
type outPicture struct {
	id   string
	data []byte // nil when id is already in paste/
	size int
}

// attachOutImages stores refs and queues them for the turn's reply line. It
// checks every ref before writing any, so a refused call leaves nothing behind.
// A picture already queued is not counted twice.
func (k *Kernel) attachOutImages(ctx context.Context, refs []string) ([]string, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	t := k.liveTurn()
	if t == nil || k.Bot == nil || k.Bot.Root == "" {
		return nil, errors.New("images: no reply to attach them to")
	}
	dir := work.BotDir(k.Bot.Root, k.Bot.Name())
	pics := make([]outPicture, 0, len(refs))
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		p, err := k.outPicture(ctx, dir, ref)
		if err != nil {
			return nil, fmt.Errorf("images: %s: %w", ref, err)
		}
		pics = append(pics, p)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	queued, total := slices.Clone(t.outImages), t.outBytes
	var added []outPicture
	for _, p := range pics {
		if slices.Contains(queued, p.id) {
			continue
		}
		queued = append(queued, p.id)
		total += p.size
		added = append(added, p)
	}
	if len(queued) > work.PastedMaxPerMessage {
		return nil, fmt.Errorf("images: at most %d pictures per reply", work.PastedMaxPerMessage)
	}
	if total > work.PastedMaxTotalBytes {
		return nil, errors.New("images: the pictures on one reply may weigh 10 MiB together")
	}
	for _, p := range added {
		if p.data == nil {
			continue
		}
		if err := work.WritePasted(dir, p.id, p.data); err != nil {
			return nil, err
		}
	}
	t.outImages, t.outBytes = queued, total
	return queued, nil
}

// outPicture checks one ref. A pasted id must already be stored; anything else
// is a path, read from inside the bot's workspace roots.
func (k *Kernel) outPicture(ctx context.Context, dir, ref string) (outPicture, error) {
	if work.ValidPastedID(ref) {
		size, err := work.PastedSize(dir, ref)
		if err != nil {
			return outPicture{}, errors.New("no such pasted picture")
		}
		return outPicture{id: ref, size: size}, nil
	}
	data, err := k.readWorkspaceFile(ctx, ref)
	if err != nil {
		return outPicture{}, err
	}
	if len(data) == 0 {
		return outPicture{}, errors.New("not a PNG, JPEG, GIF, or WebP")
	}
	mime := bot.SniffImage(data)
	if work.PastedExt(mime) == "" {
		return outPicture{}, errors.New("not a PNG, JPEG, GIF, or WebP")
	}
	if iw, ih, fits := tools.FitsModel(mime, data); !fits {
		if iw == 0 && ih == 0 {
			return outPicture{}, errors.New("cannot read image dimensions")
		}
		return outPicture{}, fmt.Errorf("%dx%d is over %d px on a side; downscale it first", iw, ih, tools.MaxImageEdge)
	}
	id, err := work.PastedNameFor(mime, data)
	if err != nil {
		return outPicture{}, err
	}
	return outPicture{id: id, data: data, size: len(data)}, nil
}

// readWorkspaceFile reads a picture at path, relative to the calling context's
// directory. The read goes through an os.Root on the workspace root that holds
// it, so neither a ".." nor a link can reach outside.
func (k *Kernel) readWorkspaceFile(ctx context.Context, path string) ([]byte, error) {
	abs, err := tools.ResolveRoot(k.cwdFor(ctx), path)
	if err != nil {
		return nil, err
	}
	for _, root := range k.allowedRoots() {
		real, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(real, abs)
		if err != nil || !filepath.IsLocal(rel) {
			continue
		}
		return readInRoot(real, rel)
	}
	return nil, errors.New("path is outside the workspace")
}

func readInRoot(root, rel string) ([]byte, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	// Stat first: opening a FIFO waits for a writer.
	st, err := r.Stat(rel)
	if err != nil {
		return nil, errors.New("no such file")
	}
	if !st.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	if st.Size() > work.PastedMaxBytes {
		return nil, errors.New("larger than 10 MiB")
	}
	f, err := r.Open(rel)
	if err != nil {
		return nil, errors.New("no such file")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, work.PastedMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > work.PastedMaxBytes {
		return nil, errors.New("larger than 10 MiB")
	}
	return data, nil
}

// takeOutImagesLocked hands the queued pictures to the line being recorded. The
// caller holds t.mu.
func (t *turn) takeOutImagesLocked() []string {
	out := t.outImages
	t.outImages, t.outBytes = nil, 0
	return out
}
