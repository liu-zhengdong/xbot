package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"xbot/internal/bm25"
	"xbot/internal/fsutil"
)

const (
	maxFactBytes    = 1024
	maxProfileBytes = 16 << 10
	maxLogBytes     = 64 << 10
	recallLimit     = 20
)

var idRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// FileStore is markdown the owner can edit, plus an index rebuilt when the markdown is newer.
type FileStore struct {
	Root string
}

// Open returns a store rooted at the xbot data directory.
func Open(root string) *FileStore { return &FileStore{Root: root} }

// Remember validates the fact and appends it to the scope's tier file.
func (s *FileStore) Remember(_ context.Context, fact Fact) error {
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
		return saveTier(dir, fact.Tier, facts)
	})
}

// Recall loads the scope and, when Text is set, ranks it with BM25.
func (s *FileStore) Recall(_ context.Context, q Query) ([]Fact, error) {
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
		if len(facts) > 200 {
			facts = facts[:200]
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
			kept := facts[:0]
			for _, f := range facts {
				if f.Text != exact {
					kept = append(kept, f)
				}
			}
			if len(kept) != len(facts) {
				if err := saveTier(dir, tier, kept); err != nil {
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
		if err := clean(botName); err != nil {
			return "", err
		}
		if err := clean(personID); err != nil {
			return "", err
		}
		return filepath.Join(s.Root, "bots", botName, "people", personID, "memory"), nil
	default:
		return "", fmt.Errorf("memory: unknown scope %q", scope)
	}
}

// ValidID reports whether id can name a person or a bot in the store.
func ValidID(id string) bool { return idRe.MatchString(id) }

func clean(id string) error {
	if !ValidID(id) {
		return fmt.Errorf("memory: bad id %q", id)
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
	switch f.Scope {
	case ScopePerson, ScopeBotPerson:
		f.Kind = KindAttribute
		if strings.TrimSpace(f.WrittenBy) == "" {
			return Fact{}, errors.New("memory: person facts require attribution")
		}
	case ScopeBot:
		if f.Kind == "" {
			f.Kind = KindWork
		}
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
		facts, err := loadTier(dir, tier, q.Scope, q.BotName, q.PersonID)
		if err != nil {
			return nil, err
		}
		all = append(all, facts...)
	}
	return all, nil
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
	mdPath := filepath.Join(dir, string(tier)+".md")
	idxPath := filepath.Join(dir, string(tier)+".index.json")
	mdInfo, mdErr := os.Stat(mdPath)
	if errors.Is(mdErr, os.ErrNotExist) {
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
	facts, err := parseMD(string(raw))
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

func saveTier(dir string, tier Tier, facts []Fact) error {
	body := renderMD(facts)
	limit := maxLogBytes
	if tier == TierProfile {
		limit = maxProfileBytes
	}
	if len(body) > limit {
		return fmt.Errorf("memory: %s tier exceeds %d bytes", tier, limit)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	mdPath := filepath.Join(dir, string(tier)+".md")
	if err := fsutil.WriteAtomic(mdPath, []byte(body), 0600); err != nil {
		return err
	}
	return writeIndex(filepath.Join(dir, string(tier)+".index.json"), facts)
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
		fmt.Fprintf(&b, "- key: %s\n  kind: %s\n  text: %s\n  by: %s\n  at: %s\n\n",
			oneLine(f.Key), f.Kind, oneLine(f.Text), oneLine(f.WrittenBy), f.Time.UTC().Format(time.RFC3339))
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
