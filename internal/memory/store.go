package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"xbot/internal/bm25"
	"xbot/internal/fsutil"
)

const (
	maxFactBytes = 1024
	maxLogBytes  = 64 << 10
	recallLimit  = 20
)

// FileStore is markdown the owner can edit, plus an index rebuilt when the markdown is newer.
type FileStore struct {
	Root string
}

// Open returns a store rooted at the xbot data directory.
func Open(root string) *FileStore { return &FileStore{Root: root} }

// Remember validates the fact and appends it to the scope's tier file.
// An empty scope is bot. person must be set explicitly.
func (s *FileStore) Remember(_ context.Context, fact Fact) error {
	if err := Migrate(s.Root); err != nil {
		return err
	}
	fact, err := normalize(fact)
	if err != nil {
		return err
	}
	dir, err := s.dir(fact.Scope, fact.BotName, fact.PersonID)
	if err != nil {
		return err
	}
	return fsutil.WithLock(filepath.Join(dir, ".lock"), func() error {
		facts, err := loadTier(dir, fact.Tier, fact.Scope, fact.BotName, fact.PersonID)
		if err != nil {
			return err
		}
		facts = append(facts, fact)
		if err := saveTier(dir, fact.Tier, facts); err != nil {
			return err
		}
		return noteSync(s.Root, opRemember, fact)
	})
}

// Recall loads the scope and, when Text is set, ranks it with BM25.
func (s *FileStore) Recall(_ context.Context, q Query) ([]Fact, error) {
	if err := Migrate(s.Root); err != nil {
		return nil, err
	}
	dir, err := s.dir(q.Scope, q.BotName, q.PersonID)
	if err != nil {
		return nil, err
	}
	var facts []Fact
	err = fsutil.WithLock(filepath.Join(dir, ".lock"), func() error {
		var err error
		facts, err = loadQuery(dir, q)
		return err
	})
	if err != nil {
		return nil, err
	}
	facts = filterQuery(facts, q)
	if strings.TrimSpace(q.Text) == "" {
		// Keep the newest 200: the snapshot's recent-log section reads the tail.
		if len(facts) > 200 {
			sort.SliceStable(facts, func(i, j int) bool { return facts[i].Time.Before(facts[j].Time) })
			facts = facts[len(facts)-200:]
		}
		return facts, nil
	}
	docs := make([]string, len(facts))
	for i, f := range facts {
		docs[i] = f.Key + " " + f.Text
	}
	hits := bm25.Top(q.Text, docs, recallLimit)
	out := make([]Fact, 0, len(hits))
	for _, h := range hits {
		out = append(out, facts[h.Index])
	}
	return out, nil
}

// Forget removes facts in the scope whose text matches exactly.
func (s *FileStore) Forget(_ context.Context, q Query, exact string) error {
	exact = strings.TrimSpace(exact)
	if exact == "" {
		return errors.New("memory: forget needs the exact fact text")
	}
	if q.Scope == "" {
		q.Scope = ScopeBot
	}
	if err := Migrate(s.Root); err != nil {
		return err
	}
	dir, err := s.dir(q.Scope, q.BotName, q.PersonID)
	if err != nil {
		return err
	}
	return fsutil.WithLock(filepath.Join(dir, ".lock"), func() error {
		tiers := []Tier{TierProfile, TierLog}
		if q.Tier != "" {
			tiers = []Tier{q.Tier}
		}
		for _, tier := range tiers {
			facts, err := loadTier(dir, tier, q.Scope, q.BotName, q.PersonID)
			if err != nil {
				return err
			}
			kept := make([]Fact, 0, len(facts))
			var removed []Fact
			for _, f := range facts {
				if f.Text == exact {
					removed = append(removed, f)
					continue
				}
				kept = append(kept, f)
			}
			if len(removed) == 0 {
				continue
			}
			if err := saveTier(dir, tier, kept); err != nil {
				return err
			}
			if q.Scope != ScopePerson {
				continue
			}
			lines, err := ReadSync(s.Root, q.PersonID)
			if err != nil {
				return err
			}
			current := foldSync(lines)
			tombstones := map[string]Fact{}
			for _, f := range removed {
				matched := false
				if tier == TierLog {
					for _, live := range current {
						if factMark(SyncLine{Fact: live, Time: live.Time, Device: live.Device}) == factMark(SyncLine{Fact: f, Time: f.Time}) {
							tombstones[live.Key] = live
							matched = true
						}
					}
				}
				if !matched {
					tombstones[f.Key] = f
				}
			}
			for _, f := range tombstones {
				f.Time = time.Now().UTC()
				if err := noteSync(s.Root, opForget, f); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *FileStore) dir(scope Scope, botName, personID string) (string, error) {
	switch scope {
	case ScopePerson:
		if err := rejectSubagent(personID); err != nil {
			return "", err
		}
		if err := clean(personID); err != nil {
			return "", err
		}
		return filepath.Join(s.Root, "people", personID, "memory"), nil
	case ScopeBot:
		if err := clean(botName); err != nil {
			return "", err
		}
		return filepath.Join(s.Root, "bots", botName, "memory"), nil
	case ScopeBotPerson:
		return "", fmt.Errorf("memory: scope %q was removed; facts now live in bot scope", scope)
	default:
		return "", fmt.Errorf("memory: unknown scope %q", scope)
	}
}

// ValidID reports whether id can name a person or a bot in the store.
func ValidID(id string) bool { return fsutil.ValidName(id) && fsutil.PortableName(id) }

func clean(id string) error {
	if !ValidID(id) {
		return fmt.Errorf("memory: bad id %q", id)
	}
	return nil
}

func rejectSubagent(id string) error {
	if strings.HasPrefix(id, "sa_") {
		return fmt.Errorf("memory: %q is a subagent, not a person", id)
	}
	return nil
}

func normalize(f Fact) (Fact, error) {
	f.Text = oneLine(f.Text)
	f.Key = oneLine(f.Key)
	if f.Text == "" {
		return Fact{}, errors.New("memory: empty fact")
	}
	if len(f.Text) > maxFactBytes {
		return Fact{}, fmt.Errorf("memory: fact exceeds %d bytes", maxFactBytes)
	}
	if f.Key == "" {
		f.Key = f.Text
	}
	f.Key = clipKey(f.Key)
	if f.Scope == "" {
		f.Scope = ScopeBot
	}
	switch f.Scope {
	case ScopePerson:
		f.Kind = KindAttribute
		if strings.TrimSpace(f.WrittenBy) == "" {
			return Fact{}, errors.New("memory: person facts require attribution")
		}
	case ScopeBot:
		if f.Kind == "" {
			f.Kind = KindWork
		}
	case ScopeBotPerson:
		return Fact{}, fmt.Errorf("memory: scope %q was removed; use bot, or person for a fact about the owner", f.Scope)
	default:
		return Fact{}, fmt.Errorf("memory: unknown scope %q", f.Scope)
	}
	if f.Tier == "" {
		f.Tier = TierLog
	}
	if f.Tier != TierProfile && f.Tier != TierLog {
		return Fact{}, fmt.Errorf("memory: unknown tier %q", f.Tier)
	}
	if f.Time.IsZero() {
		f.Time = time.Now().UTC()
	}
	f.Time = f.Time.UTC()
	return f, nil
}

func loadQuery(dir string, q Query) ([]Fact, error) {
	tiers := []Tier{TierProfile, TierLog}
	if q.Tier != "" {
		tiers = []Tier{q.Tier}
	}
	var all []Fact
	for _, tier := range tiers {
		var facts []Fact
		var err error
		if tier == TierLog {
			limit := 0
			if strings.TrimSpace(q.Text) == "" {
				limit = 200
			}
			facts, err = loadMonths(dir, q.Scope, q.BotName, q.PersonID, limit, true)
		} else {
			facts, err = loadTier(dir, tier, q.Scope, q.BotName, q.PersonID)
		}
		if err != nil {
			return nil, err
		}
		all = append(all, facts...)
	}
	return all, nil
}

func tierBase(tier Tier) string {
	if tier == TierLog {
		return "log"
	}
	if tier == TierProfile {
		return "memory"
	}
	return string(tier)
}

func filterQuery(facts []Fact, q Query) []Fact {
	out := make([]Fact, 0, len(facts))
	for _, f := range facts {
		if q.Tier != "" && f.Tier != "" && f.Tier != q.Tier {
			continue
		}
		out = append(out, f)
	}
	return out
}

func loadTier(dir string, tier Tier, scope Scope, botName, personID string) ([]Fact, error) {
	if tier == TierLog {
		return loadMonths(dir, scope, botName, personID, 0, true)
	}
	return loadNamed(dir, tierBase(tier), scope, tier, botName, personID)
}

func loadNamed(dir, base string, scope Scope, tier Tier, botName, personID string) ([]Fact, error) {
	mdPath := filepath.Join(dir, base+".md")
	idxPath := filepath.Join(dir, base+".index.json")
	mdInfo, mdErr := os.Stat(mdPath)
	if errors.Is(mdErr, os.ErrNotExist) {
		if base == "memory" {
			if _, err := os.Stat(filepath.Join(dir, "profile.md")); err == nil {
				return nil, fmt.Errorf("memory: legacy profile awaits migration: %s", dir)
			} else if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
		}
		return nil, nil
	}
	if mdErr != nil {
		return nil, mdErr
	}
	if idxInfo, err := os.Stat(idxPath); err == nil && !idxInfo.ModTime().Before(mdInfo.ModTime()) {
		raw, err := os.ReadFile(idxPath)
		if err == nil {
			var facts []Fact
			if json.Unmarshal(raw, &facts) == nil {
				return stamp(facts, scope, tier, botName, personID), nil
			}
		}
	}
	raw, err := os.ReadFile(mdPath)
	if err != nil {
		return nil, err
	}
	var facts []Fact
	if base != "memory" {
		facts, err = parseMD(string(raw))
	}
	if base == "memory" {
		facts = parseMemoryMD(string(raw), scope)
		old, _ := readLooseIndex(idxPath)
		if _, err := os.Stat(idxPath); errors.Is(err, os.ErrNotExist) {
			old, _ = readLooseIndex(filepath.Join(dir, "profile.index.json"))
		}
		used := make([]bool, len(old))
		for i, f := range facts {
			full := f.Text
			if f.WrittenBy != "" {
				full += " via " + f.WrittenBy
			}
			best := -1
			for _, text := range []string{full, f.Text} {
				for j, prev := range old {
					if used[j] || prev.Text != text || (f.WrittenBy != "" && prev.WrittenBy != f.WrittenBy) {
						continue
					}
					if best < 0 {
						best = j
					}
					if prev.Time.In(time.Local).Format("2006-01-02") == f.Time.In(time.Local).Format("2006-01-02") {
						best = j
						break
					}
				}
				if best >= 0 {
					break
				}
			}
			if best >= 0 {
				facts[i] = old[best]
				used[best] = true
			}
		}
		err = nil
	}
	if err != nil {
		return nil, err
	}
	facts = stamp(facts, scope, tier, botName, personID)
	if err := writeIndex(idxPath, facts); err != nil {
		return nil, err
	}
	return facts, nil
}

func stamp(facts []Fact, scope Scope, tier Tier, botName, personID string) []Fact {
	for i := range facts {
		facts[i].Scope = scope
		facts[i].Tier = tier
		facts[i].BotName = botName
		facts[i].PersonID = personID
	}
	return facts
}

// saveTier writes one tier. A write over the tier's cap is rejected only when
// it would grow the file: a profile already over the cap (written before the
// cap, or by sync) must still accept forgets, or it can never be consolidated.
// Sync folds pass fromSync so a remote fold is not wedged by the profile cap;
// the injection clip is the backstop there.
func saveTier(dir string, tier Tier, facts []Fact) error {
	return writeTierOpt(dir, tier, facts, true, false)
}

// saveTierOpt is the sync path. fromSync lets a remote profile fold land even
// when the merged file is over the cap; the injection clip is the backstop.
func saveTierOpt(dir string, tier Tier, facts []Fact, fromSync bool) error {
	return writeTierOpt(dir, tier, facts, true, fromSync)
}

// writeTier stores one tier. enforce is false only when a caller must write
// past the cap during migration. An enforced write over the cap is
// rejected only when it would grow the file, so a shrinking forget still lands.
func writeTier(dir string, tier Tier, facts []Fact, enforce bool) error {
	return writeTierOpt(dir, tier, facts, enforce, false)
}

func writeTierOpt(dir string, tier Tier, facts []Fact, enforce, fromSync bool) error {
	if tier == TierLog {
		return writeMonths(dir, facts, enforce && !fromSync)
	}
	return writeNamed(dir, tierBase(tier), facts, tierLimit(tier), enforce, fromSync && tier == TierProfile, tier == TierProfile)
}

func tierLimit(tier Tier) int {
	if tier == TierProfile {
		return ProfileBytes
	}
	return maxLogBytes
}

// writeNamed writes base.md and base.index.json. bypass skips the cap, which
// is how a sync fold saves an over-cap profile. enforce false skips it too.
func writeNamed(dir, base string, facts []Fact, limit int, enforce, bypass, profile bool) error {
	body := renderMD(facts)
	if base == "memory" {
		raw, err := os.ReadFile(filepath.Join(dir, base+".md"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		body = renderMemoryMD(facts, memoryIndex(string(raw)))
	}
	size := len(body)
	if base == "memory" {
		size = memoryProfileSize(body, facts)
	}
	if enforce && !bypass && size > limit {
		cur := -1
		if info, err := os.Stat(filepath.Join(dir, base+".md")); err == nil {
			cur = int(info.Size())
		}
		if len(body) > cur {
			if profile {
				return fmt.Errorf("memory: profile tier is full (%d bytes, %d byte cap). Consolidate: merge facts, forget stale ones, or move details to the log tier", max(cur, 0), limit)
			}
			return fmt.Errorf("memory: %s tier exceeds %d bytes", base, limit)
		}
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	mdPath := filepath.Join(dir, base+".md")
	if err := fsutil.WriteAtomic(mdPath, []byte(body), 0600); err != nil {
		return err
	}
	return writeIndex(filepath.Join(dir, base+".index.json"), facts)
}

func writeIndex(path string, facts []Fact) error {
	raw, err := json.MarshalIndent(facts, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return fsutil.WriteAtomic(path, raw, 0600)
}

func renderMD(facts []Fact) string {
	sort.SliceStable(facts, func(i, j int) bool {
		if facts[i].Time.Equal(facts[j].Time) {
			return facts[i].Key < facts[j].Key
		}
		return facts[i].Time.Before(facts[j].Time)
	})
	var b strings.Builder
	b.WriteString("# memory\n\n")
	for _, f := range facts {
		fmt.Fprintf(&b, "- key: %s\n  kind: %s\n  text: %s\n  by: %s\n  at: %s\n",
			oneLine(f.Key), f.Kind, oneLine(f.Text), oneLine(f.WrittenBy), f.Time.UTC().Format(time.RFC3339))
		if f.Device != "" {
			fmt.Fprintf(&b, "  device: %s\n", oneLine(f.Device))
		}
		if f.OwnerRecorded {
			b.WriteString("  owner_recorded: true\n")
		}
		if f.Untrusted {
			b.WriteString("  untrusted: true\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func parseMD(s string) ([]Fact, error) {
	var facts []Fact
	var cur *Fact
	flush := func() {
		if cur == nil {
			return
		}
		cur.Text = strings.TrimSpace(cur.Text)
		if cur.Text != "" {
			facts = append(facts, *cur)
		}
		cur = nil
	}
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "- key: ") {
			flush()
			cur = &Fact{Key: strings.TrimSpace(strings.TrimPrefix(line, "- key: "))}
			continue
		}
		if cur == nil {
			continue
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "kind: "):
			cur.Kind = Kind(strings.TrimSpace(strings.TrimPrefix(trimmed, "kind: ")))
		case strings.HasPrefix(trimmed, "text: "):
			cur.Text = strings.TrimPrefix(trimmed, "text: ")
		case strings.HasPrefix(trimmed, "by: "):
			cur.WrittenBy = strings.TrimSpace(strings.TrimPrefix(trimmed, "by: "))
		case strings.HasPrefix(trimmed, "at: "):
			t, err := time.Parse(time.RFC3339, strings.TrimSpace(strings.TrimPrefix(trimmed, "at: ")))
			if err != nil {
				return nil, fmt.Errorf("memory: bad time %q", trimmed)
			}
			cur.Time = t
		case strings.HasPrefix(trimmed, "device: "):
			cur.Device = strings.TrimSpace(strings.TrimPrefix(trimmed, "device: "))
		case trimmed == "untrusted: true":
			cur.Untrusted = true
		case trimmed == "owner_recorded: true":
			cur.OwnerRecorded = true
		}
	}
	flush()
	return facts, nil
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}

// DefaultKey returns the key assigned when a fact has no custom key.
func DefaultKey(text string) string { return clipKey(text) }

func clipKey(s string) string {
	if len(s) <= 80 {
		return s
	}
	s = s[:80]
	for !utf8.ValidString(s) && len(s) > 0 {
		s = s[:len(s)-1]
	}
	return s
}

// memoryProfileSize includes custom keys shown by the kernel but absent from memory.md.
func memoryProfileSize(body string, facts []Fact) int {
	size := len(body)
	for _, f := range facts {
		if f.Key != DefaultKey(f.Text) {
			size += len(f.Key) + 2
		}
	}
	return size
}
