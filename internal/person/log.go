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
	// Grant is set only by a local owner action. Sync clears it.
	// Owner accounts come from the newest granted entry, so a later local
	// action can remove an account. A remote line may carry accounts and
	// still not grant them on this machine.
	Grant bool `json:"grant,omitempty"`
}

// maxSkew is how far a merged identity timestamp may sit ahead of this clock.
const maxSkew = 2 * time.Minute

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
	e := LogEntry{Time: at.UTC(), Bot: strings.TrimSpace(bot), Device: device, Person: p, Grant: true}
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
// A timestamp past now plus a small skew is capped. Illegal lines are dropped.
// Remote accounts are stored on the log line and do not grant ownership here.
// Bot directories are not touched. A real change wakes the fast sync path.
func MergeLog(root string, remote []LogEntry) error {
	now := time.Now()
	home := deviceOf(root)
	byID := map[string][]LogEntry{}
	for _, e := range remote {
		id := e.Person.ID
		if !idRe.MatchString(id) {
			continue
		}
		e, ok := acceptRemote(e, now, home)
		if !ok {
			continue
		}
		e.Person.ID = id
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
	changed := false
	for _, id := range ids {
		ok, err := mergeOne(root, id, byID[id])
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

func mergeOne(root, id string, remote []LogEntry) (bool, error) {
	changed := false
	err := fsutil.WithLock(filepath.Join(root, "people", id, ".lock"), func() error {
		local, err := ReadLog(root, id)
		if err != nil {
			return err
		}
		if len(local) == 0 {
			if cur, err := Load(root, id); err == nil {
				local = []LogEntry{{Time: time.Unix(0, 0).UTC(), Device: deviceOf(root), Grant: true, Person: cur}}
				if err := appendLog(root, local[0]); err != nil {
					return err
				}
				changed = true
			}
		}
		have := map[string]bool{}
		for _, e := range local {
			have[logKey(e)] = true
		}
		for _, e := range remote {
			e.Person.ID = id
			e.Grant = false
			if have[logKey(e)] {
				continue
			}
			if err := appendLog(root, e); err != nil {
				return err
			}
			have[logKey(e)] = true
			changed = true
		}
		return rewriteIdentity(root, id)
	})
	return changed, err
}

func rewriteIdentity(root, id string) error {
	lines, err := ReadLog(root, id)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		return nil
	}
	p := foldIdentity(lines, deviceOf(root))
	raw, err := fsutil.Marshal(p)
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(filepath.Join(root, "people", id, "identity.json"), raw, 0600)
}

func foldIdentity(lines []LogEntry, home string) Person {
	best := lines[0]
	grantIdx := -1
	var grantAt time.Time
	homeIdx := -1
	var homeAt time.Time
	anyGrant := false
	for i, e := range lines {
		if e.Time.After(best.Time) {
			best = e
		}
		if e.Grant {
			anyGrant = true
			if grantIdx < 0 || e.Time.After(grantAt) || (e.Time.Equal(grantAt) && i > grantIdx) {
				grantIdx = i
				grantAt = e.Time
			}
		}
		if e.Device == home || e.Device == "local" || e.Device == "" {
			if homeIdx < 0 || e.Time.After(homeAt) || (e.Time.Equal(homeAt) && i > homeIdx) {
				homeIdx = i
				homeAt = e.Time
			}
		}
	}
	// Display name, timezone, and accounts follow a local grant.
	// A remote line can be newer and still not rename the owner.
	var p Person
	switch {
	case grantIdx >= 0:
		p = lines[grantIdx].Person
		p.Accounts = append([]Account(nil), lines[grantIdx].Person.Accounts...)
	case !anyGrant && homeIdx >= 0:
		p = lines[homeIdx].Person
		p.Accounts = append([]Account(nil), lines[homeIdx].Person.Accounts...)
	default:
		p = best.Person
		p.Accounts = nil
	}
	sort.Slice(p.Accounts, func(i, j int) bool {
		if p.Accounts[i].Link == p.Accounts[j].Link {
			return p.Accounts[i].ExternalID < p.Accounts[j].ExternalID
		}
		return p.Accounts[i].Link < p.Accounts[j].Link
	})
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
