package kernel

import (
	"fmt"
	"os"
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

// pastedNote names the pictures on a line, relative to the workspace, so the
// model can open any of them with view_image. Empty when the line had none.
func (k *Kernel) pastedNote(names []string) string {
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
		b.WriteString("paste/" + name)
	}
	// One wording serves both places the note appears. The turn answering this
	// line already has the bytes, so it needs no tool call; a later turn has only
	// the file, so it opens the path. Saying both at once keeps the assembled
	// context identical in either case, which is what makes the request
	// cacheable across steps.
	if _, sees := k.vision(); sees {
		b.WriteString("。写在主人这条消息上；这一轮请求已附上图片，早先的消息则用 view_image 打开这个路径。")
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
	imgs, _ := k.pastedImages(names)
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
	path, err := work.PastedPath(work.BotDir(k.Bot.Root, k.Bot.Name()), name)
	if err != nil {
		return "", nil, err
	}
	data, err := os.ReadFile(path)
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
func (k *Kernel) pastedImages(names []string) ([]provider.Image, []string) {
	if _, sees := k.vision(); !sees {
		return nil, nil
	}
	t := k.curTurn()
	if t == nil {
		return nil, nil
	}
	t.pasteMu.Lock()
	defer t.pasteMu.Unlock()
	var imgs []provider.Image
	var used []string
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
		used = append(used, name)
	}
	return imgs, used
}
