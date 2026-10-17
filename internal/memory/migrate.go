package memory

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"xbot/internal/fsutil"
)

const migrationNoteKey = "migration-profile-cap"

var migrateOnce struct {
	mu    sync.Mutex
	roots map[string]*sync.Once
}

// Migrate folds legacy bots/<name>/people/<id>/memory into bots/<name>/memory.
// It re-walks every bot on each process start, once per root in this process:
// migrated folders are renamed to memory.bak, so the walk is idempotent.
// A bad source is logged and left in place; it does not fail the caller, the
// bot next door, or process start.
//
// Only a person id listed in that bot's owners is merged. Other people are
// renamed to memory.bak without merging, because bot scope is visible to every
// sender. A missing or unreadable config is not an empty owner list: that bot
// is left untouched. Facts keep their timestamps, provenance, and kind.
func Migrate(root string) error {
	if root == "" {
		return nil
	}
	root = filepath.Clean(root)
	onceFor(root).Do(func() { migrateRoot(root) })
	return nil
}

func onceFor(root string) *sync.Once {
	migrateOnce.mu.Lock()
	defer migrateOnce.mu.Unlock()
	if migrateOnce.roots == nil {
		migrateOnce.roots = map[string]*sync.Once{}
	}
	o := migrateOnce.roots[root]
	if o == nil {
		o = &sync.Once{}
		migrateOnce.roots[root] = o
	}
	return o
}

func migrateRoot(root string) {
	defer migrateLayout(root)
	bots := filepath.Join(root, "bots")
	entries, err := os.ReadDir(bots)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		log.Printf("memory: migrate %s: %v", bots, err)
		return
	}
	for _, ent := range entries {
		if !ent.IsDir() || !ValidID(ent.Name()) {
			continue
		}
		_ = migrateBot(root, ent.Name())
	}
}

// migrateBot reports whether any source for this bot was left unmerged.
func migrateBot(root, botName string) bool {
	people := filepath.Join(root, "bots", botName, "people")
	entries, err := os.ReadDir(people)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err != nil {
		log.Printf("memory: migrate %s: %v", people, err)
		return true
	}
	var sources []string
	for _, ent := range entries {
		if !ent.IsDir() || !ValidID(ent.Name()) {
			continue
		}
		src := filepath.Join(people, ent.Name(), "memory")
		st, err := os.Stat(src)
		if errors.Is(err, os.ErrNotExist) || (err == nil && !st.IsDir()) {
			continue
		}
		if err != nil {
			log.Printf("memory: migrate %s: %v", src, err)
			return true
		}
		sources = append(sources, src)
	}
	if len(sources) == 0 {
		return false
	}
	dest := filepath.Join(root, "bots", botName, "memory")
	failed := false
	err = fsutil.WithLock(filepath.Join(dest, ".lock"), func() error {
		failed = migrateLocked(root, botName, dest, sources)
		return nil
	})
	if err != nil {
		log.Printf("memory: migrate %s: %v", dest, err)
		return true
	}
	return failed
}

func migrateLocked(root, botName, dest string, sources []string) bool {
	owners, err := botOwners(root, botName)
	if err != nil {
		log.Printf("memory: migrate %s: %v", filepath.Join(root, "bots", botName, "config.json"), err)
		return true
	}
	// Stat again under the dest lock. Another process may have renamed the
	// directory already; opening it would recreate an empty memory dir and
	// then rename that to memory.bak.N.
	var live []string
	for _, src := range sources {
		if dirExists(src) {
			live = append(live, src)
		}
	}
	if len(live) == 0 {
		return false
	}
	profileBase := legacyProfileBase(dest)
	profile, err := loadNamed(dest, profileBase, ScopeBot, TierProfile, botName, "")
	if err != nil {
		log.Printf("memory: migrate %s profile: %v", dest, err)
		return true
	}
	if err := migrateLogs(dest, ScopeBot, botName, ""); err != nil {
		return true
	}
	logFacts, err := loadTier(dest, TierLog, ScopeBot, botName, "")
	if err != nil {
		log.Printf("memory: migrate %s log: %v", dest, err)
		return true
	}

	failed := false
	var incomingProfile, incomingLog []Fact
	var merged, parked []string
	for _, src := range live {
		if !dirExists(src) {
			continue
		}
		id := filepath.Base(filepath.Dir(src))
		if !owners[id] {
			// Not an owner: set the folder aside without reading it, so a
			// corrupt file still moves to memory.bak.
			log.Printf("memory: migrate %s: %s is not an owner; leaving the legacy folder as memory.bak", botName, src)
			parked = append(parked, src)
			continue
		}
		p, l, err := loadLegacy(src, botName)
		if err != nil {
			log.Printf("memory: migrate %s: %v", src, err)
			failed = true
			continue
		}
		incomingProfile = append(incomingProfile, p...)
		incomingLog = append(incomingLog, l...)
		merged = append(merged, src)
	}
	profile, logFacts, profileChanged, logChanged := mergeLegacy(botName, profile, logFacts, incomingProfile, incomingLog)
	if profileChanged {
		if err := writeNamed(dest, profileBase, profile, ProfileBytes, true, false, true); err != nil {
			log.Printf("memory: migrate %s profile: %v", dest, err)
			return true
		}
	}
	if logChanged {
		if err := writeMonths(dest, logFacts, false); err != nil {
			return true
		}
	}

	for _, src := range append(merged, parked...) {
		if !dirExists(src) {
			continue
		}
		if err := backupDir(src); err != nil {
			log.Printf("memory: migrate %s: %v", src, err)
			failed = true
		}
	}
	return failed
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// botOwners reads bots/<name>/config.json. A missing, unreadable, or invalid
// file is an error. An empty owners list is a real answer: nobody, so every
// legacy directory is set aside instead of merged.
func botOwners(root, botName string) (map[string]bool, error) {
	path := filepath.Join(root, "bots", botName, "config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg struct {
		Owners []string `json:"owners"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, id := range cfg.Owners {
		out[id] = true
	}
	return out, nil
}

func loadLegacy(dir, botName string) (profile, logFacts []Fact, err error) {
	err = fsutil.WithLock(filepath.Join(dir, ".lock"), func() error {
		var err error
		profile, err = loadNamed(dir, legacyProfileBase(dir), ScopeBot, TierProfile, botName, "")
		if err != nil {
			return err
		}
		logFacts, err = loadNamed(dir, "log", ScopeBot, TierLog, botName, "")
		if err == nil {
			extra, e := loadNamed(dir, "log.overflow", ScopeBot, TierLog, botName, "")
			err = e
			logFacts = append(logFacts, extra...)
		}
		if err != nil {
			return err
		}
		extra, err := readLooseIndex(filepath.Join(dir, ".index.json"))
		if err != nil {
			return err
		}
		for _, f := range extra {
			f = asBotFact(f, botName)
			if f.Tier == TierProfile {
				profile = append(profile, f)
				continue
			}
			f.Tier = TierLog
			logFacts = append(logFacts, f)
		}
		return nil
	})
	return profile, logFacts, err
}

func readLooseIndex(path string) ([]Fact, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var facts []Fact
	if err := json.Unmarshal(raw, &facts); err != nil {
		return nil, fmt.Errorf("memory: %s: %w", path, err)
	}
	return facts, nil
}

// asBotFact moves a legacy fact into bot scope. Kind is kept as stored, including
// empty. Resolve treats an empty kind as an attribute, not a working agreement.
func asBotFact(f Fact, botName string) Fact {
	f.Scope = ScopeBot
	f.BotName = botName
	f.PersonID = ""
	f.Text = oneLine(f.Text)
	f.Key = oneLine(f.Key)
	if f.Key == "" {
		f.Key = clipKey(f.Text)
	} else {
		f.Key = clipKey(f.Key)
	}
	if f.Time.IsZero() {
		f.Time = time.Unix(0, 0).UTC()
	}
	f.Time = f.Time.UTC()
	if f.Tier != TierProfile {
		f.Tier = TierLog
	}
	return f
}

func mergeLegacy(botName string, profile, logFacts, incomingProfile, incomingLog []Fact) ([]Fact, []Fact, bool, bool) {
	seen := map[string]bool{}
	for i := range profile {
		profile[i] = asBotFact(profile[i], botName)
		profile[i].Tier = TierProfile
		seen[factID(profile[i])] = true
	}
	for i := range logFacts {
		logFacts[i] = asBotFact(logFacts[i], botName)
		logFacts[i].Tier = TierLog
	}
	profileChanged := false
	logChanged := false
	profileOver := memoryProfileSize(renderMemoryMD(append([]Fact(nil), profile...), ""), profile) > ProfileBytes
	var overflow []Fact
	for _, f := range incomingProfile {
		f = asBotFact(f, botName)
		f.Tier = TierProfile
		if f.Text == "" || seen[factID(f)] {
			continue
		}
		if profileOver {
			f.Tier = TierLog
			overflow = append(overflow, f)
			continue
		}
		trial := append(append([]Fact{}, profile...), f)
		if memoryProfileSize(renderMemoryMD(trial, ""), trial) > ProfileBytes {
			f.Tier = TierLog
			overflow = append(overflow, f)
			continue
		}
		profile = trial
		seen[factID(f)] = true
		profileChanged = true
	}
	var incoming []Fact
	for _, f := range append(incomingLog, overflow...) {
		f = asBotFact(f, botName)
		f.Tier = TierLog
		if f.Text != "" && !seen[factID(f)] {
			incoming = append(incoming, f)
		}
	}
	before := len(logFacts)
	logFacts = mergeLogs(logFacts, incoming)
	logChanged = len(logFacts) != before

	if len(overflow) > 0 && !hasKey(profile, migrationNoteKey) {
		note := Fact{
			Key:       migrationNoteKey,
			Kind:      KindWork,
			Scope:     ScopeBot,
			Tier:      TierLog,
			BotName:   botName,
			WrittenBy: botName,
			Text:      fmt.Sprintf("migration: %d profile fact(s) from the removed bot-person scope moved to the log tier because the %d-byte profile cap would have been exceeded", len(overflow), ProfileBytes),
			Time:      time.Now().UTC(),
		}
		logFacts = mergeLogs(logFacts, []Fact{note})
		logChanged = len(logFacts) != before
	}
	return profile, logFacts, profileChanged, logChanged
}

func factID(f Fact) string {
	return f.Key + "\x00" + f.Text + "\x00" + f.Time.UTC().Format(time.RFC3339Nano) + "\x00" + f.WrittenBy + "\x00" + string(f.Kind) + "\x00" + f.Device + "\x00" + boolMark(f.Untrusted) + "\x00" + boolMark(f.OwnerRecorded)
}

func boolMark(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func hasKey(facts []Fact, key string) bool {
	for _, f := range facts {
		if f.Key == key {
			return true
		}
	}
	return false
}

func backupDir(src string) error {
	// Missing means another runner already renamed it. Do not create the
	// directory (WithLock would) and do not invent memory.bak.N.
	if !dirExists(src) {
		return nil
	}
	base := src + ".bak"
	dest := base
	for i := 1; i < 1000; i++ {
		if _, err := os.Stat(dest); errors.Is(err, os.ErrNotExist) {
			return os.Rename(src, dest)
		}
		dest = fmt.Sprintf("%s.%d", base, i)
	}
	return fmt.Errorf("memory: too many backups for %s", src)
}
