package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"xbot/internal/fsutil"
)

const (
	opRemember = "remember"
	opForget   = "forget"
)

// SyncLine is one record in the person-memory log (people/<id>/memory/sync.jsonl).
// The log is local and append-only; read-time folds pick the winning record per key.
type SyncLine struct {
	Time   time.Time `json:"time"`
	Bot    string    `json:"bot,omitempty"`
	Device string    `json:"device"`
	Op     string    `json:"op"`
	// Untrusted marks a fact that came from an untrusted turn.
	Untrusted bool `json:"untrusted,omitempty"`
	// OwnerRecorded is kept so lines written before node sync was retired still
	// load; nothing sets it any more.
	OwnerRecorded bool `json:"owner_recorded,omitempty"`
	Fact          Fact `json:"fact"`
}

// ReadSync returns the person-memory log. A missing file is empty.
func ReadSync(root, personID string) ([]SyncLine, error) {
	if err := clean(personID); err != nil {
		return nil, err
	}
	var out []SyncLine
	err := fsutil.ReadJSONL(syncPath(root, personID), func(line []byte) error {
		var e SyncLine
		if err := json.Unmarshal(line, &e); err != nil {
			return err
		}
		out = append(out, e)
		return nil
	})
	return out, err
}

// foldSync returns the winning fact per key after replaying the log in order:
// the newest record wins, and a forget record drops the fact.
func foldSync(lines []SyncLine) []Fact {
	type slot struct {
		ln  SyncLine
		idx int
		ok  bool
	}
	order := []string{}
	best := map[string]*slot{}
	for i, ln := range lines {
		key := ln.Fact.Key
		if key == "" || (ln.Op != opRemember && ln.Op != opForget) {
			continue
		}
		cur := best[key]
		if cur == nil {
			best[key] = &slot{ln: ln, idx: i, ok: true}
			order = append(order, key)
			continue
		}
		if ln.Time.After(cur.ln.Time) {
			cur.ln = ln
			cur.idx = i
			continue
		}
		// An owner-recorded copy of the same line replaces a non-owner copy.
		if ln.Time.Equal(cur.ln.Time) && ln.OwnerRecorded && !cur.ln.OwnerRecorded &&
			ln.Device == cur.ln.Device && ln.Op == cur.ln.Op && ln.Fact.Text == cur.ln.Fact.Text {
			cur.ln = ln
			cur.idx = i
		}
	}
	var out []Fact
	for _, key := range order {
		ln := best[key].ln
		if ln.Op == opForget {
			continue
		}
		f := ln.Fact
		f.Key = key
		f.Scope = ScopePerson
		f.Time = ln.Time.UTC()
		if f.WrittenBy == "" {
			f.WrittenBy = ln.Bot
		}
		if ln.Device != "" {
			f.Device = ln.Device
		}
		if ln.OwnerRecorded {
			f.Untrusted = false
			f.OwnerRecorded = true
			f.Text = OwnerProvenance(f.Text, f.Device)
		} else if ln.Untrusted || f.Untrusted || f.OwnerRecorded {
			f.OwnerRecorded = false
			f.Untrusted = true
			f.Text = Provenance(f.Text, f.WrittenBy, f.Device)
		}
		out = append(out, f)
	}
	return out
}

// noteSync appends one record for a person-scope fact to the local log.
func noteSync(root string, op string, fact Fact) error {
	if fact.Scope != ScopePerson {
		return nil
	}
	bot := strings.TrimSpace(fact.WrittenBy)
	if bot == "" {
		bot = strings.TrimSpace(fact.BotName)
	}
	if err := appendSync(root, SyncLine{
		Time:      fact.Time.UTC(),
		Bot:       bot,
		Device:    deviceOf(root),
		Op:        op,
		Untrusted: fact.Untrusted,
		Fact:      fact,
	}); err != nil {
		return err
	}
	notifyChange(root)
	return nil
}

func appendSync(root string, ln SyncLine) error {
	if err := clean(ln.Fact.PersonID); err != nil {
		return err
	}
	if ln.Time.IsZero() {
		ln.Time = time.Now().UTC()
	}
	ln.Time = ln.Time.UTC()
	if ln.Device == "" {
		ln.Device = deviceOf(root)
	}
	if ln.Op == "" {
		ln.Op = opRemember
	}
	return fsutil.AppendJSONL(syncPath(root, ln.Fact.PersonID), ln)
}

func syncPath(root, id string) string {
	return filepath.Join(root, "people", id, "memory", "sync.jsonl")
}

func factMark(ln SyncLine) string {
	if ln.Fact.Tier == TierLog {
		f := ln.Fact
		f.Device = ln.Device
		f.OwnerRecorded = f.OwnerRecorded || ln.OwnerRecorded
		f.Untrusted = f.Untrusted || ln.Untrusted
		text := logText(f)
		return text + "\x00" + ln.Time.UTC().Format("2006-01-02 15:04") + "\x00log"
	}
	return ln.Fact.Key + "\x00" + ln.Fact.Text + "\x00" + ln.Time.UTC().Format(time.RFC3339Nano) + "\x00" + string(ln.Fact.Tier)
}

func deviceOf(root string) string {
	raw, err := os.ReadFile(filepath.Join(root, "node-id"))
	s := strings.TrimSpace(string(raw))
	if err != nil || s == "" || strings.ContainsAny(s, "/\\\n\r") || len(s) > 128 {
		return "local"
	}
	return s
}
