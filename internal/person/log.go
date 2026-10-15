package person

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"xbot/internal/fsutil"
)

// LogEntry is one append-only identity record. The relay merges these by time.
type LogEntry struct {
	Time   time.Time `json:"time"`
	Bot    string    `json:"bot,omitempty"`
	Device string    `json:"device"`
	Person Person    `json:"person"`
}

// ReadLog returns the identity log for one person. A missing file is empty.
func ReadLog(root, id string) ([]LogEntry, error) {
	if !idRe.MatchString(id) {
		return nil, errors.New("person: bad id")
	}
	var out []LogEntry
	err := fsutil.ReadJSONL(logPath(root, id), func(line []byte) error {
		var e LogEntry
		if err := json.Unmarshal(line, &e); err != nil {
			return err
		}
		out = append(out, e)
		return nil
	})
	return out, err
}

// Record appends an identity line and refreshes identity.json from the newest line.
func Record(root, bot, device string, at time.Time, p Person) error {
	if !idRe.MatchString(p.ID) {
		return errors.New("person: bad id")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	if strings.TrimSpace(device) == "" {
		device = deviceOf(root)
	}
	e := LogEntry{Time: at.UTC(), Bot: strings.TrimSpace(bot), Device: device, Person: p}
	if err := appendLog(root, e); err != nil {
		return err
	}
	if err := rewriteIdentity(root, p.ID); err != nil {
		return err
	}
	notifyChange(root)
	return nil
}

// ExportLog returns every identity line on this data root.
func ExportLog(root string) ([]LogEntry, error) {
	if err := MergeLog(root, nil); err != nil {
		return nil, err
	}
	ids, err := localIDs(root)
	if err != nil {
		return nil, err
	}
	var out []LogEntry
	for _, id := range ids {
		lines, err := ReadLog(root, id)
		if err != nil {
			return nil, err
		}
		out = append(out, lines...)
	}
	return out, nil
}

// MergeLog folds remote identity lines into this data root.
// The newest timestamp wins. An equal timestamp keeps the line already stored here.
// Bot directories are not touched.
func MergeLog(root string, remote []LogEntry) error {
	byID := map[string][]LogEntry{}
	for _, e := range remote {
		id := e.Person.ID
		if !idRe.MatchString(id) {
			return errors.New("person: bad id")
		}
		byID[id] = append(byID[id], e)
	}
	ids, err := localIDs(root)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	for id := range byID {
		if !seen[id] {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		if err := mergeOne(root, id, byID[id]); err != nil {
			return err
		}
	}
	return nil
}

func mergeOne(root, id string, remote []LogEntry) error {
	return fsutil.WithLock(filepath.Join(root, "people", id, ".lock"), func() error {
		local, err := ReadLog(root, id)
		if err != nil {
			return err
		}
		if len(local) == 0 {
			if cur, err := Load(root, id); err == nil {
				local = []LogEntry{{Time: time.Unix(0, 0).UTC(), Device: "local", Person: cur}}
				if err := appendLog(root, local[0]); err != nil {
					return err
				}
			}
		}
		have := map[string]bool{}
		for _, e := range local {
			have[logKey(e)] = true
		}
		for _, e := range remote {
			e.Time = e.Time.UTC()
			e.Person.ID = id
			if have[logKey(e)] {
				continue
			}
			if err := appendLog(root, e); err != nil {
				return err
			}
			have[logKey(e)] = true
		}
		return rewriteIdentity(root, id)
	})
}

func rewriteIdentity(root, id string) error {
	lines, err := ReadLog(root, id)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		return nil
	}
	p := foldIdentity(lines)
	raw, err := fsutil.Marshal(p)
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(filepath.Join(root, "people", id, "identity.json"), raw, 0600)
}

func foldIdentity(lines []LogEntry) Person {
	best := lines[0]
	accounts := map[Account]struct{}{}
	for _, a := range best.Person.Accounts {
		accounts[a] = struct{}{}
	}
	for _, e := range lines[1:] {
		for _, a := range e.Person.Accounts {
			accounts[a] = struct{}{}
		}
		if e.Time.After(best.Time) {
			best = e
		}
	}
	p := best.Person
	p.Accounts = p.Accounts[:0]
	for a := range accounts {
		p.Accounts = append(p.Accounts, a)
	}
	sort.Slice(p.Accounts, func(i, j int) bool {
		if p.Accounts[i].Link == p.Accounts[j].Link {
			return p.Accounts[i].ExternalID < p.Accounts[j].ExternalID
		}
		return p.Accounts[i].Link < p.Accounts[j].Link
	})
	if p.DisplayName == "" || p.Timezone == "" {
		for i := len(lines) - 1; i >= 0; i-- {
			if p.DisplayName == "" {
				p.DisplayName = lines[i].Person.DisplayName
			}
			if p.Timezone == "" {
				p.Timezone = lines[i].Person.Timezone
			}
		}
	}
	return p
}

func appendLog(root string, e LogEntry) error {
	if !idRe.MatchString(e.Person.ID) {
		return errors.New("person: bad id")
	}
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	e.Time = e.Time.UTC()
	if e.Device == "" {
		e.Device = deviceOf(root)
	}
	return fsutil.AppendJSONL(logPath(root, e.Person.ID), e)
}

func logPath(root, id string) string {
	return filepath.Join(root, "people", id, "identity.jsonl")
}

func logKey(e LogEntry) string {
	raw, _ := json.Marshal(e.Person.Accounts)
	return e.Time.UTC().Format(time.RFC3339Nano) + "\x00" + e.Bot + "\x00" + e.Device + "\x00" + e.Person.ID + "\x00" + e.Person.DisplayName + "\x00" + e.Person.Timezone + "\x00" + string(raw)
}

func localIDs(root string) ([]string, error) {
	dir := filepath.Join(root, "people")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, ent := range entries {
		if ent.IsDir() && idRe.MatchString(ent.Name()) {
			ids = append(ids, ent.Name())
		}
	}
	return ids, nil
}

func deviceOf(root string) string {
	raw, err := os.ReadFile(filepath.Join(root, "node-id"))
	s := strings.TrimSpace(string(raw))
	if err != nil || s == "" || strings.ContainsAny(s, "/\\\n\r") || len(s) > 128 {
		return "local"
	}
	return s
}
