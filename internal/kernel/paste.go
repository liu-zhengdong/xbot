package kernel

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"xbot/internal/bot"
	"xbot/internal/compact"
	"xbot/internal/provider"
	"xbot/internal/work"
)

// Pasted pictures in the model context.
//
// The transcript records which pictures a line carried, by file name. Two
// things follow from that:
//
//   - The turn that answers the line sees the bytes, so "看这张图" works without
//     a tool call. That is the whole point of pasting instead of typing a path.
//   - Later turns see only a note naming the file. Resending every pasted
//     picture on every turn would grow the request without bound, and the note
//     lets the model open the one it needs with view_image.
const (
	// pastedImagesTurn caps the pictures attached to one model call. It matches
	// the agent's own per-turn budget: a fifth would be dropped there anyway.
	pastedImagesTurn = 4
	// maxPastedBytesTurn caps their combined size, the same 10 MiB view_image
	// allows to reach the model.
	maxPastedBytesTurn = 10 << 20
)

// pastedNote names the pictures on a line so the model can open any of them
// with view_image. Empty when the line had none.
func (k *Kernel) pastedNote(names []string) string {
	return k.pastedNoteIn(k.Bot, names)
}

// pastedNoteIn is pastedNote for a named bot's pictures.
func (k *Kernel) pastedNoteIn(owner *bot.Bot, names []string) string {
	if len(names) == 0 {
		return ""
	}
	var b strings.Builder
	if len(names) == 1 {
		b.WriteString("[主人的图] ")
	} else {
		fmt.Fprintf(&b, "[主人的 %d 张图] ", len(names))
	}
	for i, name := range names {
		if i > 0 {
			b.WriteString("、")
		}
		b.WriteString(pastedPathIn(owner, name))
	}
	if owner != nil && k.Bot != nil && owner.Name() != k.Bot.Name() {
		b.WriteString("（这些图在对方 Bot 的工作区里，这里的 view_image 打不开）")
	}
	// A cross-bot read names the other bot's workspace, which this bot's
	// view_image may not open. Say so instead of offering a path that fails:
	// the workspace boundary is deliberate and not worth widening for this.

	// One wording serves both places the note appears. The turn answering this
	// line already has the bytes, so it needs no tool call; a later turn has only
	// the file, so it opens the path. Saying both at once keeps the assembled
	// context identical in either case, which is what makes the request
	// cacheable across steps.
	if _, sees := k.vision(); sees {
		b.WriteString("。写在主人这条消息上；这一轮请求已附上图片，早先的消息则用 view_image 打开这个路径。路径是绝对的，Bot 换过工作目录也照样打得开。")
	} else {
		b.WriteString("。当前模型看不了图；需要时请主人说图里有什么。")
	}
	return b.String()
}

// attachEventImages puts this turn's pictures on the message being answered and
// reports the tokens they add to the usage estimate. The bytes are read from the
// workspace and live only in this request: the transcript keeps the names, and
// the agent drops the bytes when the turn ends.
func (k *Kernel) attachEventImages(msgs []provider.Message, names []string) ([]provider.Message, int) {
	if len(names) == 0 || len(msgs) == 0 {
		return msgs, 0
	}
	at := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == provider.RoleUser {
			at = i
			break
		}
	}
	if at < 0 {
		return msgs, 0
	}
	imgs := k.pastedImages(names)
	if len(imgs) == 0 {
		return msgs, 0
	}
	out := append([]provider.Message(nil), msgs...)
	out[at].Images = imgs
	return out, compact.EstimateMessage(out[at]) - compact.EstimateMessage(msgs[at])
}

// readPasted loads one stored picture from the bot workspace.
func (k *Kernel) readPasted(name string) (string, []byte, error) {
	if k == nil || k.Bot == nil || k.Bot.Root == "" {
		return "", nil, os.ErrNotExist
	}
	data, err := work.ReadPasted(work.BotDir(k.Bot.Root, k.Bot.Name()), name)
	if err != nil {
		return "", nil, err
	}
	mime := bot.SniffImage(data)
	if mime == "" {
		return "", nil, os.ErrNotExist
	}
	return mime, data, nil
}

// pastedImages loads the named pictures for one request, within the count and
// size caps of the whole turn. It returns the pictures and the names it read, so
// a caller stores only what was sent. A name that no longer resolves is skipped:
// the line keeps it, and the note tells the model the path.
//
// The caps are the turn's, not the message's: a second absorbed message must not
// start a fresh four pictures beside the ones already in the request.
func (k *Kernel) pastedImages(names []string) []provider.Image {
	if _, sees := k.vision(); !sees {
		return nil
	}
	t := k.curTurn()
	if t == nil {
		return nil
	}
	t.pasteMu.Lock()
	defer t.pasteMu.Unlock()
	var imgs []provider.Image
	for _, name := range names {
		if t.pasteSeen >= pastedImagesTurn || t.pasteBytes >= maxPastedBytesTurn {
			break
		}
		mime, data, err := k.readPasted(name)
		if err != nil {
			continue
		}
		if t.pasteBytes+len(data) > maxPastedBytesTurn {
			continue
		}
		t.pasteSeen++
		t.pasteBytes += len(data)
		imgs = append(imgs, provider.Image{MIME: mime, Data: data})
	}
	return imgs
}

// withPastedNote puts the note naming a line's pictures into the text a model
// or a summary reads. One function so the assembled context, a summary of an
// old range, and expand all name the pictures the same way: a picture's file
// outlives the message, and a summary that dropped the name would leave the
// model with a line about a picture it can no longer find.
func (k *Kernel) withPastedNote(content string, names []string) string {
	return joinNote(content, k.pastedNote(names))
}

// joinNote puts a note after content with the blank line the model expects
// between two blocks, or alone when there is no text.
func joinNote(content, note string) string {
	note = strings.TrimSpace(note)
	if note == "" {
		return content
	}
	if strings.TrimSpace(content) == "" {
		return note
	}
	return content + "\n\n" + note
}

// stripPastedNote removes a note this package appended to a message, so the
// transcript keeps the owner's own words. The note is added again when the line
// is assembled, which is why it must not be stored.
func stripPastedNote(content, note string) string {
	note = strings.TrimSpace(note)
	if note == "" {
		return content
	}
	if content == note {
		return ""
	}
	if rest, ok := strings.CutSuffix(content, "\n\n"+note); ok {
		return rest
	}
	return content
}

// pastedPathIn is the path the note gives for one stored picture. It is
// absolute: view_image resolves a relative path against whatever directory the
// bot has cd-ed into, so a relative name would stop working the moment the bot
// moved, and the note outlives that move. Absolute paths still pass the
// workspace check view_image applies to every local path.
//
// It takes the bot the picture belongs to, not the reading kernel: a cross-bot
// expand must name that bot's workspace.
func pastedPathIn(b *bot.Bot, name string) string {
	if b == nil || b.Root == "" {
		return "paste/" + name
	}
	return filepath.Join(work.BotDir(b.Root, b.Name()), work.PastedDir, name)
}

// withPastedNoteIn is withPastedNote for another bot's line.
func (k *Kernel) withPastedNoteIn(b *bot.Bot, content string, names []string) string {
	return joinNote(content, k.pastedNoteIn(b, names))
}
