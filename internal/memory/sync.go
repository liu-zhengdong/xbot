package memory

import (
	"encoding/json"
	"errors"
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

// SyncLine is one append-only person-memory record. The relay merges these by time.
type SyncLine struct {
	Time   time.Time `json:"time"`
	Bot    string    `json:"bot,omitempty"`
	Device string    `json:"device"`
	Op     string    `json:"op"`
	// Untrusted is set when the line arrived from another node, or from an untrusted turn.
	Untrusted bool `json:"untrusted,omitempty"`
	// OwnerRecorded is set only when the authenticated peer that delivered the
	// line is this device, and this machine has linked that peer.
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

// ExportSync reads every person-memory log under the data root.
// Facts that were stored before the log existed are appended first.
func ExportSync(root string) ([]SyncLine, error) {
	if err := MergeSync(root, nil); err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "people")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []SyncLine
	for _, ent := range entries {
		if !ent.IsDir() || !ValidID(ent.Name()) {
			continue
		}
		lines, err := ReadSync(root, ent.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, lines...)
	}
	return out, nil
}

// MergeSync folds remote person-memory lines into this data root.
// The peer is unknown, so every remote line stays untrusted.
// The newest timestamp wins for each key. An equal timestamp keeps the local line.
// A timestamp past now plus a small skew is capped. Illegal lines are dropped.
// Files under bots/ are left alone. A real change wakes the fast sync path.
func MergeSync(root string, remote []SyncLine) error {
	return MergeFrom(root, "", remote)
}

// MergeFrom is MergeSync for a bundle delivered by an authenticated peer.
// peer is that node's device id. A line is owner-recorded only when its device
// is this peer and the peer is linked locally. Lines relayed by the peer stay untrusted.
func MergeFrom(root, peer string, remote []SyncLine) error {
	now := time.Now()
	home := deviceOf(root)
	linked := linkedDevices(root)
	peer = strings.TrimSpace(peer)
	byID := map[string][]SyncLine{}
	for _, ln := range remote {
		id := ln.Fact.PersonID
		if err := clean(id); err != nil {
			continue
		}
		if ln.Fact.Scope != "" && ln.Fact.Scope != ScopePerson {
			continue
		}
		ln, ok := acceptRemote(ln, now, home, peer, linked)
		if !ok {
			continue
		}
		ln.Fact.PersonID = id
		byID[id] = append(byID[id], ln)
	}
	ids := map[string]bool{}
	entries, err := os.ReadDir(filepath.Join(root, "people"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, ent := range entries {
		if ent.IsDir() && ValidID(ent.Name()) {
			ids[ent.Name()] = true
		}
	}
	for id := range byID {
		ids[id] = true
	}
	changed := false
	for id := range ids {
		ok, err := mergePerson(root, id, byID[id])
		if err != nil {
			return err
		}
		changed = changed || ok
	}
	if changed {
		notifyChange(root)
	}
	return nil
}

func mergePerson(root, id string, remote []SyncLine) (bool, error) {
	dir := filepath.Join(root, "people", id, "memory")
	changed := false
	err := fsutil.WithLock(filepath.Join(dir, ".lock"), func() error {
		local, err := ReadSync(root, id)
		if err != nil {
			return err
		}
		extra, err := backfill(dir, id, local)
		if err != nil {
			return err
		}
		for _, ln := range extra {
			if err := appendSync(root, ln); err != nil {
				return err
			}
			local = append(local, ln)
			changed = true
		}
		have := map[string]bool{}
		ownerSeen := map[string]bool{}
		for _, ln := range local {
			key := syncKey(ln)
			have[key] = true
			if ln.OwnerRecorded {
				ownerSeen[key] = true
			}
		}
		for _, ln := range remote {
			ln.Fact.PersonID = id
			ln.Fact.Scope = ScopePerson
			key := syncKey(ln)
			if have[key] && (!ln.OwnerRecorded || ownerSeen[key]) {
				continue
			}
			if err := appendSync(root, ln); err != nil {
				return err
			}
			local = append(local, ln)
			have[key] = true
			if ln.OwnerRecorded {
				ownerSeen[key] = true
			}
			changed = true
		}
		if len(local) == 0 {
			return nil
		}
		return writeFold(dir, id, local)
	})
	return changed, err
}

func backfill(dir, id string, local []SyncLine) ([]SyncLine, error) {
	have := map[string]bool{}
	for _, ln := range local {
		have[factMark(ln)] = true
	}
	var extra []SyncLine
	for _, tier := range []Tier{TierProfile, TierLog} {
		facts, err := loadTier(dir, tier, ScopePerson, "", id)
		if err != nil {
			return nil, err
		}
		for _, f := range facts {
			if f.Untrusted {
				continue
			}
			ln := SyncLine{Time: f.Time.UTC(), Bot: f.WrittenBy, Device: "local", Op: opRemember, Fact: f}
			if have[factMark(ln)] {
				continue
			}
			extra = append(extra, ln)
			have[factMark(ln)] = true
		}
	}
	return extra, nil
}

func writeFold(dir, id string, lines []SyncLine) error {
	byTier := map[Tier][]Fact{}
	winners := foldSync(lines)
	for _, f := range winners {
		tier := f.Tier
		if tier == "" {
			tier = TierLog
		}
		byTier[tier] = append(byTier[tier], f)
	}
	for _, tier := range []Tier{TierProfile, TierLog} {
		facts := byTier[tier]
		if facts == nil {
			facts = []Fact{}
		}
		if err := saveTierOpt(dir, tier, facts, true); err != nil {
			return err
		}
	}
	return nil
}

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
		// A later direct delivery of the same line can replace a relayed copy.
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

func syncKey(ln SyncLine) string {
	return ln.Time.UTC().Format(time.RFC3339Nano) + "\x00" + ln.Bot + "\x00" + ln.Device + "\x00" + ln.Op + "\x00" + ln.Fact.Key + "\x00" + ln.Fact.Text + "\x00" + string(ln.Fact.Tier)
}

func factMark(ln SyncLine) string {
	return ln.Fact.Key + "\x00" + ln.Fact.Text + "\x00" + ln.Time.UTC().Format(time.RFC3339Nano) + "\x00" + string(ln.Fact.Tier)
}

func linkedDevices(root string) map[string]bool {
	out := map[string]bool{}
	raw, err := os.ReadFile(filepath.Join(root, "nodes.json"))
	if err != nil {
		return out
	}
	var doc struct {
		Nodes []struct {
			ID string `json:"id"`
		} `json:"nodes"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return out
	}
	for _, n := range doc.Nodes {
		id := strings.TrimSpace(n.ID)
		if id == "" || id == "local" || id == "relay" || strings.ContainsAny(id, "/\\\n\r") {
			continue
		}
		out[id] = true
	}
	return out
}

func deviceOf(root string) string {
	raw, err := os.ReadFile(filepath.Join(root, "node-id"))
	s := strings.TrimSpace(string(raw))
	if err != nil || s == "" || strings.ContainsAny(s, "/\\\n\r") || len(s) > 128 {
		return "local"
	}
	return s
}
