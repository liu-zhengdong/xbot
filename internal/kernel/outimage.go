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

// errReplySent refuses pictures for a turn whose reply is already written: a
// call that finished late (a detached tool job) cannot put them in that bubble,
// and putting them in whatever turn runs now would show them under an
// unrelated reply.
var errReplySent = errors.New("images: the reply this call belongs to was already sent, so the pictures were not attached and no note went out; call notify_owner with images again from the reply that should show them")

// imageTurn is the turn the call belongs to. A tool job keeps the real turn in
// callTurnKey (turnKey there is a frozen copy); an ordinary call carries it in
// turnKey. Only a call with neither falls back to the turn running now.
func (k *Kernel) imageTurn(ctx context.Context) *turn {
	if ctx != nil {
		if t, ok := ctx.Value(callTurnKey{}).(*turn); ok && t != nil {
			return t
		}
		if t, ok := ctx.Value(turnKey{}).(*turn); ok && t != nil {
			return t
		}
	}
	return k.liveTurn()
}

// attachOutImages stores refs and queues them for the turn's reply line. It
// checks every ref before writing any, so a refused call leaves nothing behind.
// A picture already queued is not counted twice.
//
// The caps bind while reading, not after: at most PastedMaxPerMessage distinct
// refs are read, a repeated ref is read once, and a file is refused by its size
// before its bytes are loaded when it would push the reply past the byte cap.
// What one call holds in memory is therefore bounded by that cap.
func (k *Kernel) attachOutImages(ctx context.Context, refs []string) ([]string, error) {
	var uniq []string
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref != "" && !slices.Contains(uniq, ref) {
			uniq = append(uniq, ref)
		}
		if len(uniq) > work.PastedMaxPerMessage {
			return nil, fmt.Errorf("images: at most %d pictures per reply", work.PastedMaxPerMessage)
		}
	}
	if len(uniq) == 0 {
		return nil, nil
	}
	t := k.imageTurn(ctx)
	if t == nil || k.Bot == nil || k.Bot.Root == "" {
		return nil, errors.New("images: no reply to attach them to")
	}
	t.mu.Lock()
	sent := t.answered || t.finished
	queued, total := slices.Clone(t.outImages), t.outBytes
	t.mu.Unlock()
	if sent {
		return nil, errReplySent
	}
	dir := work.BotDir(k.Bot.Root, k.Bot.Name())
	var pics []outPicture
	for _, ref := range uniq {
		p, err := k.outPicture(ctx, dir, ref, work.PastedMaxTotalBytes-total)
		if err != nil {
			return nil, fmt.Errorf("images: %s: %w", ref, err)
		}
		if slices.Contains(queued, p.id) {
			continue
		}
		queued = append(queued, p.id)
		total += p.size
		pics = append(pics, p)
		if len(queued) > work.PastedMaxPerMessage {
			return nil, fmt.Errorf("images: at most %d pictures per reply", work.PastedMaxPerMessage)
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	// The turn may have moved on while the files were read.
	if t.answered || t.finished {
		return nil, errReplySent
	}
	queued, total = slices.Clone(t.outImages), t.outBytes
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
		return nil, errOverBudget
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

var errOverBudget = errors.New("images: the pictures on one reply may weigh 10 MiB together")

// outPicture checks one ref, reading at most budget bytes. A pasted id must
// already be stored and is checked by its contents, the same as a path: paste/
// is in the writable workspace, so its name alone proves nothing. Anything else
// is a path, read from inside the bot's workspace roots.
func (k *Kernel) outPicture(ctx context.Context, dir, ref string, budget int) (outPicture, error) {
	var data []byte
	pasted := work.ValidPastedID(ref)
	if pasted {
		size, err := work.PastedSize(dir, ref)
		if err != nil {
			return outPicture{}, errors.New("no such pasted picture")
		}
		if size > budget {
			return outPicture{}, errOverBudget
		}
		if data, err = work.ReadPasted(dir, ref); err != nil {
			return outPicture{}, errors.New("no such pasted picture")
		}
	} else {
		var err error
		if data, err = k.readWorkspaceFile(ctx, ref, budget); err != nil {
			return outPicture{}, err
		}
	}
	if len(data) > budget {
		return outPicture{}, errOverBudget
	}
	mime := bot.SniffImage(data)
	if len(data) == 0 || work.PastedExt(mime) == "" {
		return outPicture{}, errors.New("not a PNG, JPEG, GIF, or WebP")
	}
	if iw, ih, fits := tools.FitsModel(mime, data); !fits {
		if iw == 0 && ih == 0 {
			return outPicture{}, errors.New("cannot read image dimensions")
		}
		return outPicture{}, fmt.Errorf("%dx%d is over %d px on a side; downscale it first", iw, ih, tools.MaxImageEdge)
	}
	if pasted {
		// Already stored; the bytes were only needed for the check.
		if work.PastedExt(mime) != filepath.Ext(ref) {
			return outPicture{}, errors.New("contents do not match the name")
		}
		return outPicture{id: ref, size: len(data)}, nil
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
func (k *Kernel) readWorkspaceFile(ctx context.Context, path string, budget int) ([]byte, error) {
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
		return readInRoot(real, rel, budget)
	}
	return nil, errors.New("path is outside the workspace")
}

func readInRoot(root, rel string, budget int) ([]byte, error) {
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
	if st.Size() > int64(budget) {
		return nil, errOverBudget
	}
	f, err := r.Open(rel)
	if err != nil {
		return nil, errors.New("no such file")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(min(budget, work.PastedMaxBytes))+1))
	if err != nil {
		return nil, err
	}
	if len(data) > budget {
		return nil, errOverBudget
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
