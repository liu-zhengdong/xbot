// Package notify is the owner's notification seam: an append-only outbox plus
// an optional desktop sink. Sending on the user's behalf is a different path.
package notify

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os/exec"
	"time"

	"xbot/internal/fsutil"
)

// Note is one notification waiting for the owner.
type Note struct {
	ID   string    `json:"id"`
	Text string    `json:"text"`
	Time time.Time `json:"time"`
}

type record struct {
	Op   string    `json:"op"`
	ID   string    `json:"id,omitempty"`
	Text string    `json:"text,omitempty"`
	Time time.Time `json:"time,omitempty"`
}

// Outbox is bots/<name>/outbox.jsonl.
type Outbox struct {
	path string
}

// Open attaches an outbox file. The file is created on the first write.
func Open(path string) *Outbox { return &Outbox{path: path} }

// Append stores one note and returns it.
func (o *Outbox) Append(text string, at time.Time) (Note, error) {
	if text == "" {
		return Note{}, errors.New("notify: empty note")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	n := Note{ID: newID(), Text: text, Time: at.UTC()}
	err := fsutil.AppendJSONL(o.path, record{Op: "note", ID: n.ID, Text: n.Text, Time: n.Time})
	if err != nil {
		return n, err
	}
	emitNote(o.path, n)
	return n, nil
}

// Unread folds the log and returns notes that have not been marked read.
func (o *Outbox) Unread() ([]Note, error) {
	var order []string
	notes := map[string]Note{}
	read := map[string]bool{}
	err := fsutil.ReadJSONL(o.path, func(line []byte) error {
		var rec record
		if err := json.Unmarshal(line, &rec); err != nil {
			return err
		}
		switch rec.Op {
		case "note":
			if rec.ID == "" {
				return nil
			}
			if _, ok := notes[rec.ID]; !ok {
				order = append(order, rec.ID)
			}
			notes[rec.ID] = Note{ID: rec.ID, Text: rec.Text, Time: rec.Time}
		case "read":
			if rec.ID != "" {
				read[rec.ID] = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var out []Note
	for _, id := range order {
		if !read[id] {
			out = append(out, notes[id])
		}
	}
	return out, nil
}

// MarkRead records that the owner has seen these notes.
func (o *Outbox) MarkRead(ids []string) error {
	for _, id := range ids {
		if id == "" {
			continue
		}
		if err := fsutil.AppendJSONL(o.path, record{Op: "read", ID: id}); err != nil {
			return err
		}
	}
	return nil
}

// Desktop tries notify-send and ignores a missing binary.
func Desktop(ctx context.Context, title, body string) error {
	if _, err := exec.LookPath("notify-send"); err != nil {
		return nil
	}
	cmd := exec.CommandContext(ctx, "notify-send", title, body)
	return cmd.Run()
}

func newID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "nt_local"
	}
	return "nt_" + hex.EncodeToString(b[:])
}
